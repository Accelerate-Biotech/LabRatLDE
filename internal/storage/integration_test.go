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

// sealFixture is SPEC-002's shared fixture: sample S →used→ run R
// →wasGeneratedBy→ dataset D (100 readings from instrument I), with R then
// moved to a given state.
type sealFixture struct {
	tenant, s, r, d, i string // lineage node ids
	runID, instID      string
}

// insertReadings takes tenant, instrument, dataset and count.
const insertReadings = `
	INSERT INTO instrument_readings (tenant_id, instrument_id, dataset_id, ts, channel, value)
	SELECT $1, $2, $3, now() + make_interval(secs => g), 'ct', g FROM generate_series(1, $4::int) g`

func newSealFixture(t *testing.T, db *storage.DB, state string) sealFixture {
	t.Helper()
	ctx := context.Background()
	f := sealFixture{tenant: newTenant(t, db)}
	err := db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error {
		for _, n := range []struct {
			id   *string
			kind string
		}{{&f.s, "sample"}, {&f.r, "run"}, {&f.d, "dataset"}, {&f.i, "instrument"}} {
			if err := tx.QueryRow(ctx, insertNode, f.tenant, n.kind).Scan(n.id); err != nil {
				return err
			}
		}
		var expID string
		if err := tx.QueryRow(ctx, `INSERT INTO experiments (tenant_id, slug, title, created_by)
			VALUES ($1, 'exp', 'exp', 'it@lab') RETURNING id`, f.tenant).Scan(&expID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO runs (tenant_id, experiment_id, node_id, created_by)
			VALUES ($1, $2, $3, 'it@lab') RETURNING id`, f.tenant, expID, f.r).Scan(&f.runID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO instruments (tenant_id, node_id, name, kind)
			VALUES ($1, $2, 'qpcr-1', 'qpcr') RETURNING id`, f.tenant, f.i).Scan(&f.instID); err != nil {
			return err
		}
		for _, e := range [][3]string{{f.s, f.r, "used"}, {f.r, f.d, "wasGeneratedBy"}, {f.i, f.d, "wasAttributedTo"}} {
			if _, err := tx.Exec(ctx, insertEdge, f.tenant, e[0], e[1], e[2]); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, insertReadings, f.tenant, f.instID, f.d, 100)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE runs SET state = $2 WHERE id = $1`, f.runID, state); err != nil {
		t.Fatal(err)
	}
	return f
}

// insertNode takes tenant and kind; insertEdge takes tenant, from, to and relation.
const (
	insertNode = `INSERT INTO lineage_nodes (tenant_id, kind, label, created_by)
		VALUES ($1, $2, $2, 'it@lab') RETURNING id`
	insertEdge = `INSERT INTO lineage_edges (tenant_id, from_id, to_id, relation, created_by)
		VALUES ($1, $2, $3, $4, 'it@lab')`
)

// wantCode checks that err carries SQLSTATE code, or is nil when code is "".
func wantCode(t *testing.T, what string, err error, code string) {
	t.Helper()
	if got := storage.ErrorCode(err); got != code || (code == "" && err != nil) {
		t.Errorf("%s: want SQLSTATE %q, got %v", what, code, err)
	}
}

// SPEC-002-R01
func TestSealedSet(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		state  string
		sealed bool
	}{{"signed", true}, {"aborted", true}, {"completed", false}} {
		f := newSealFixture(t, db, tc.state)
		for name, c := range map[string]struct {
			node string
			want bool
		}{"R": {f.r, tc.sealed}, "D": {f.d, tc.sealed}, "S": {f.s, false}, "I": {f.i, false}} {
			var got bool
			err := db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT lineage_node_sealed($1)`, c.node).Scan(&got)
			})
			if err != nil || got != c.want {
				t.Errorf("run %s: sealed(%s) = %v, want %v (err %v)", tc.state, name, got, c.want, err)
			}
		}
	}
}

// SPEC-002-R02, SPEC-002-R03, SPEC-002-R04
func TestSealedNodesGainNoInputsOrOutputs(t *testing.T) {
	db := openTestDB(t)
	f := newSealFixture(t, db, "signed")
	ctx := context.Background()
	const fresh = "fresh" // stands for a node created alongside the edge
	for _, tc := range []struct {
		name     string
		kind     string // of the fresh node
		from, to string
		relation string
		want     string
	}{
		{"R02 signed run uses new reagent", "reagent", fresh, f.r, "used", storage.CodeSealed},
		{"R02 sealed dataset derived from new node", "analysis", fresh, f.d, "wasDerivedFrom", storage.CodeSealed},
		{"R03 new dataset generated by signed run", "dataset", f.r, fresh, "wasGeneratedBy", storage.CodeSealed},
		{"R04 new analysis uses sealed dataset", "analysis", f.d, fresh, "used", ""},
	} {
		err := db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error {
			var id string
			if err := tx.QueryRow(ctx, insertNode, f.tenant, tc.kind).Scan(&id); err != nil {
				return err
			}
			from, to := tc.from, tc.to
			if from == fresh {
				from = id
			}
			if to == fresh {
				to = id
			}
			_, err := tx.Exec(ctx, insertEdge, f.tenant, from, to, tc.relation)
			return err
		})
		wantCode(t, tc.name, err, tc.want)
	}
}

// SPEC-002-R05
func TestSealedNodesAreImmutable(t *testing.T) {
	db := openTestDB(t)
	f := newSealFixture(t, db, "signed")
	ctx := context.Background()
	// As the owner, so no grant stands in the way: only the trigger does.
	for _, tc := range []struct{ name, stmt, node, want string }{
		{"update sealed dataset", `UPDATE lineage_nodes SET attrs = '{"x":1}' WHERE id = $1`, f.d, storage.CodeSealed},
		{"update signed run node", `UPDATE lineage_nodes SET label = 'x' WHERE id = $1`, f.r, storage.CodeSealed},
		{"delete sealed dataset", `DELETE FROM lineage_nodes WHERE id = $1`, f.d, storage.CodeSealed},
		{"update unsealed sample", `UPDATE lineage_nodes SET attrs = '{"x":1}' WHERE id = $1`, f.s, ""},
	} {
		_, err := db.Pool.Exec(ctx, tc.stmt, tc.node)
		wantCode(t, tc.name, err, tc.want)
	}
}

// SPEC-002-R06, SPEC-002-R07
func TestReadingsOnlyFromCreatingTransaction(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	signed := newSealFixture(t, db, "signed")
	_, err := db.Pool.Exec(ctx, insertReadings, signed.tenant, signed.instID, signed.d, 1)
	wantCode(t, "R06 owner appends to sealed dataset", err, storage.CodeSealed)

	f := newSealFixture(t, db, "running")
	var d3 string
	err = db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, insertNode, f.tenant, "dataset").Scan(&d3); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, insertEdge, f.tenant, f.r, d3, "wasGeneratedBy"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, insertReadings, f.tenant, f.instID, d3, 1)
		return err
	})
	wantCode(t, "R07 creating transaction writes readings", err, "")
	err = db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertReadings, f.tenant, f.instID, d3, 1)
		return err
	})
	wantCode(t, "R07 later transaction appends readings", err, storage.CodeSealed)
}

// SPEC-002-R08
func TestAppRoleCannotUpdateUnusedTables(t *testing.T) {
	db := openTestDB(t)
	f := newSealFixture(t, db, "completed")
	ctx := context.Background()
	const insufficientPrivilege = "42501"
	for _, tc := range []struct{ stmt, want string }{
		{`UPDATE experiments SET title = 'x'`, insufficientPrivilege},
		{`UPDATE lineage_nodes SET label = 'x'`, insufficientPrivilege},
		{`UPDATE instruments SET name = 'x'`, insufficientPrivilege},
		{`UPDATE runs SET params = '{"x":1}'`, ""},
	} {
		err := db.WithTenant(ctx, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, tc.stmt); return err })
		wantCode(t, tc.stmt, err, tc.want)
	}
}
