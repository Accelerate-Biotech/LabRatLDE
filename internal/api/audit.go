package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
)

// listAuditEvents pages through the tenant's chain.
//
//	GET /v1/audit/events?after=<seq>&limit=<n≤1000>
func (s *Server) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	events := []audit.Event{}
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		return audit.Scan(ctx, tx, p.Tenant, after, limit, func(ev audit.Event) error {
			events = append(events, ev)
			return nil
		})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// exportAudit streams the full chain as NDJSON audit.Records, one per line,
// for offline verification with nothing but SHA-256 (see tools/verify_audit.py).
func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) error {
		return audit.Scan(ctx, tx, p.Tenant, 0, 0, func(ev audit.Event) error { return enc.Encode(ev.Record()) })
	})
	if err != nil {
		s.log.Error("audit export aborted", "tenant", p.Tenant, "err", err)
	}
}

// verifyAudit re-derives the chain twice, independently: once in Go from raw
// rows, once inside PostgreSQL via audit_verify_chain(). Both must agree.
func (s *Server) verifyAudit(w http.ResponseWriter, r *http.Request) {
	ctx, p := r.Context(), principal(r.Context())
	type dbReport struct {
		Events   int64  `json:"events"`
		HeadSeq  int64  `json:"head_seq"`
		HeadHash string `json:"head_hash"`
		BrokenAt *int64 `json:"broken_at,omitempty"`
	}
	var (
		engine audit.Report
		db     dbReport
	)
	err := s.db.WithTenant(ctx, p.Tenant, func(tx pgx.Tx) (err error) {
		if engine, err = audit.VerifyTenant(ctx, tx, p.Tenant); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT events, head_seq, head_hash, broken_at FROM audit_verify_chain($1)`, p.Tenant).
			Scan(&db.Events, &db.HeadSeq, &db.HeadHash, &db.BrokenAt)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	agree := engine.Valid == (db.BrokenAt == nil) && engine.Head.Seq == db.HeadSeq && engine.Head.Hash.String() == db.HeadHash
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":    engine.Valid && db.BrokenAt == nil && agree,
		"engine":   engine,
		"database": db,
		"agree":    agree,
	})
}
