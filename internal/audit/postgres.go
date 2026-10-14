package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Append seals e onto the tenant's chain inside tx.
//
// It must run in the same transaction as the change it records, so the event
// and the mutation commit or roll back together. Concurrent appends for one
// tenant are serialised by a transaction-scoped advisory lock; the
// audit_events insert trigger independently re-checks linkage and recomputes
// the digest, so a buggy or malicious client cannot write a forged event.
func Append(ctx context.Context, tx pgx.Tx, tenantID string, e Entry) (Event, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('lde.audit:' || $1, 0))`, tenantID); err != nil {
		return Event{}, fmt.Errorf("audit: lock chain: %w", err)
	}

	head := Head{Hash: Genesis}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT seq, hash FROM audit_chain_heads WHERE tenant_id = $1`, tenantID).Scan(&head.Seq, &raw)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// First event for this tenant: chain from Genesis.
	case err != nil:
		return Event{}, fmt.Errorf("audit: read chain head: %w", err)
	default:
		if head.Hash, err = HashFromBytes(raw); err != nil {
			return Event{}, err
		}
	}

	// Timestamps come from the database clock, not the app server's, so
	// every node in a cluster stamps events from one trusted source.
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Event{}, fmt.Errorf("audit: read clock: %w", err)
	}

	ev, err := Seal(tenantID, head, now, e)
	if err != nil {
		return Event{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events
		  (tenant_id, seq, occurred_at, actor, action, entity_type, entity_id, reason, payload, prev_hash, hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		ev.TenantID, ev.Seq, ev.OccurredAt, ev.Actor, ev.Action, ev.EntityType, ev.EntityID,
		ev.Reason, string(ev.Payload), ev.PrevHash[:], ev.Hash[:])
	if err != nil {
		return Event{}, fmt.Errorf("audit: insert event: %w", err)
	}
	return ev, nil
}

// Scan streams the tenant's events in chain order, starting after afterSeq.
// limit <= 0 means no limit.
func Scan(ctx context.Context, tx pgx.Tx, tenantID string, afterSeq int64, limit int, fn func(Event) error) error {
	q := `
		SELECT tenant_id::text, seq, occurred_at, actor, action, entity_type, entity_id, reason, payload, prev_hash, hash
		FROM audit_events
		WHERE tenant_id = $1 AND seq > $2
		ORDER BY seq`
	args := []any{tenantID, afterSeq}
	if limit > 0 {
		q += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("audit: query events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			ev            Event
			payload       string
			prev, current []byte
		)
		if err := rows.Scan(&ev.TenantID, &ev.Seq, &ev.OccurredAt, &ev.Actor, &ev.Action, &ev.EntityType,
			&ev.EntityID, &ev.Reason, &payload, &prev, &current); err != nil {
			return fmt.Errorf("audit: scan event: %w", err)
		}
		ev.OccurredAt = ev.OccurredAt.UTC()
		ev.Payload = []byte(payload)
		if ev.PrevHash, err = HashFromBytes(prev); err != nil {
			return err
		}
		if ev.Hash, err = HashFromBytes(current); err != nil {
			return err
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return rows.Err()
}

// VerifyTenant re-derives the tenant's entire chain in Go, trusting nothing
// the database says about it beyond the raw rows.
func VerifyTenant(ctx context.Context, tx pgx.Tx, tenantID string) (Report, error) {
	var v Verifier
	err := Scan(ctx, tx, tenantID, 0, 0, v.Next)
	var brk *BreakError
	if err != nil && !errors.As(err, &brk) {
		return Report{}, err
	}
	return v.Report(err), nil
}
