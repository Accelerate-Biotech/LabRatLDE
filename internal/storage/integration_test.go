package storage_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/audit"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

// These tests exercise the guarantees that live in the database itself.
// Run them with: make test-integration  (needs `docker compose up -d`).
func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	url := os.Getenv("LDE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LDE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return db
}

func newTenant(t *testing.T, db *storage.DB) string {
	t.Helper()
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = (b[6]&0x0f)|0x40, (b[8]&0x3f)|0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	if err := db.EnsureTenant(context.Background(), id, "t-"+id[:8], "test"); err != nil {
		t.Fatal(err)
	}
	return id
}

func appendN(t *testing.T, db *storage.DB, tenant string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		err := db.WithTenant(ctx, tenant, func(tx pgx.Tx) error {
			_, err := audit.Append(ctx, tx, tenant, audit.Entry{
				Actor: "it@lab", Action: "test.event", EntityType: "test", EntityID: fmt.Sprint(i),
				Reason: "ünïcödé reason", Payload: map[string]any{"i": i, "html": "<b>&</b>"},
			})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// SPEC-001-R07
func TestDatabaseAndEngineAgreeOnChain(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	appendN(t, db, tenant, 5)

	ctx := context.Background()
	err := db.WithTenant(ctx, tenant, func(tx pgx.Tx) error {
		rep, err := audit.VerifyTenant(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if !rep.Valid || rep.Events != 5 {
			t.Fatalf("engine verify: %+v", rep)
		}
		var headHash string
		var broken *int64
		if err := tx.QueryRow(ctx, `SELECT head_hash, broken_at FROM audit_verify_chain($1)`, tenant).Scan(&headHash, &broken); err != nil {
			return err
		}
		if broken != nil || headHash != rep.Head.Hash.String() {
			t.Fatalf("database verify disagrees: head=%s broken=%v engine=%s", headHash, broken, rep.Head.Hash)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// SPEC-001-R05
func TestDatabaseRejectsForgedEvent(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	err := db.WithTenant(ctx, tenant, func(tx pgx.Tx) error {
		ev, err := audit.Seal(tenant, audit.Head{Hash: audit.Genesis}, time.Now(), audit.Entry{
			Actor: "mallory", Action: "forge", EntityType: "x", EntityID: "1",
		})
		if err != nil {
			return err
		}
		ev.Actor = "alice" // content no longer matches hash
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_events (tenant_id, seq, occurred_at, actor, action, entity_type, entity_id, reason, payload, prev_hash, hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			ev.TenantID, ev.Seq, ev.OccurredAt, ev.Actor, ev.Action, ev.EntityType, ev.EntityID, ev.Reason,
			string(ev.Payload), ev.PrevHash[:], ev.Hash[:])
		return err
	})
	if storage.ErrorCode(err) != storage.CodeAuditLink {
		t.Fatalf("want SQLSTATE %s, got %v", storage.CodeAuditLink, err)
	}
}

// SPEC-001-R06
func TestLedgerIsAppendOnly(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	appendN(t, db, tenant, 1)
	ctx := context.Background()
	for _, stmt := range []string{
		`UPDATE audit_events SET actor = 'mallory'`,
		`DELETE FROM audit_events`,
		`TRUNCATE audit_events`,
	} {
		err := db.WithTenant(ctx, tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
		if err == nil {
			t.Errorf("%q succeeded as the app role", stmt)
		}
	}
	// Even the owner, bypassing WithTenant, is stopped by the triggers.
	if _, err := db.Pool.Exec(ctx, `DELETE FROM audit_events WHERE tenant_id = $1`, tenant); storage.ErrorCode(err) != storage.CodeAppendOnly {
		t.Errorf("owner DELETE: want SQLSTATE %s, got %v", storage.CodeAppendOnly, err)
	}
}

// SPEC-001-R08
func TestRowLevelSecurityIsolatesTenants(t *testing.T) {
	db := openTestDB(t)
	a, b := newTenant(t, db), newTenant(t, db)
	appendN(t, db, a, 2)
	ctx := context.Background()
	var visible int
	err := db.WithTenant(ctx, b, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, a).Scan(&visible)
	})
	if err != nil || visible != 0 {
		t.Fatalf("tenant b saw %d of tenant a's events (err %v)", visible, err)
	}
}
