package storage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
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
	testers.Store(id, newPrincipal(t, db, id, "it@lab", "human", "scientist", "reviewer", "qa", "admin"))
	return id
}

// testers maps each newTenant tenant to its it@lab principal.
var testers sync.Map

// as returns the principal the tests act as in tenant.
func as(tenant string) storage.Principal {
	p, _ := testers.Load(tenant)
	return p.(storage.Principal)
}

// newPrincipal creates a principal (subject = handle at https://idp.test)
// holding roles, granted by itself, as the owner, with the audit events its
// rows need.
func newPrincipal(t *testing.T, db *storage.DB, tenant, handle, kind string, roles ...string) storage.Principal {
	t.Helper()
	ctx := context.Background()
	var p storage.Principal
	err := db.WithOwner(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, insertPrincipal, tenant, kind, handle).Scan(&p.ID); err != nil {
			return err
		}
		_, err := audit.Append(ctx, tx, tenant, audit.Entry{
			Actor: "system:test", Action: "principal.created", EntityType: "principal", EntityID: p.ID,
			Payload: map[string]any{"handle": handle, "kind": kind},
		})
		for _, role := range roles {
			if err != nil {
				return err
			}
			_, err = grantRole(ctx, tx, tenant, "system:test", p.ID, p.ID, role, "fixture", true)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// grantRole inserts a tenant-wide grant of role to principalID and, when
// audited, its role.granted event. As lde_app the trigger replaces grantedBy.
func grantRole(ctx context.Context, tx pgx.Tx, tenant, actor, principalID, grantedBy, role, reason string, audited bool) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO role_grants (tenant_id, principal_id, role, reason, granted_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, tenant, principalID, role, reason, grantedBy).Scan(&id); err != nil {
		return "", err
	}
	if !audited {
		return id, nil
	}
	_, err := audit.Append(ctx, tx, tenant, audit.Entry{
		Actor: actor, Action: "role.granted", EntityType: "role_grant", EntityID: id, Reason: reason,
		Payload: map[string]any{"principal_id": principalID, "role": role},
	})
	return id, err
}

func appendN(t *testing.T, db *storage.DB, tenant string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
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
	ctx := context.Background()
	var fixtureEvents int64 // newTenant's principal and role grants
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, tenant).Scan(&fixtureEvents); err != nil {
		t.Fatal(err)
	}
	appendN(t, db, tenant, 5)

	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		rep, err := audit.VerifyTenant(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if !rep.Valid || rep.Events != fixtureEvents+5 {
			t.Fatalf("engine verify: %+v, want %d events", rep, fixtureEvents+5)
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
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
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
		err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt); return err })
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
	err := db.WithTenant(ctx, b, as(b), func(tx pgx.Tx) error {
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
	err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
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
		pvID, _ := newProtocolVersion(t, tx, f.tenant, expID, 1, "# p\n")
		if err := tx.QueryRow(ctx, `INSERT INTO runs (tenant_id, experiment_id, node_id, protocol_version_id, created_by)
			VALUES ($1, $2, $3, $4, 'it@lab') RETURNING id`, f.tenant, expID, f.r, pvID).Scan(&f.runID); err != nil {
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
			err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
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
		err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
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
	} {
		_, err := db.Pool.Exec(ctx, tc.stmt, tc.node)
		wantCode(t, tc.name, err, tc.want)
	}
	// An unsealed node is editable, but only inside a committed change set.
	err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, f.tenant, "it@lab", "fixture"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE lineage_nodes SET attrs = '{"x":1}' WHERE id = $1`, f.s)
		return err
	})
	wantCode(t, "update unsealed sample", err, "")
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
	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
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
	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
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
		{`UPDATE experiments SET slug = 'x'`, insufficientPrivilege},
		{`UPDATE lineage_nodes SET kind = 'analysis'`, insufficientPrivilege},
		{`UPDATE instruments SET name = 'x'`, insufficientPrivilege},
		{`UPDATE experiments SET title = 'x'`, storage.CodeAppendOnly},
		{`UPDATE lineage_nodes SET label = 'x'`, storage.CodeAppendOnly},
		{`UPDATE runs SET state = 'running'`, ""},
	} {
		err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error { _, err := tx.Exec(ctx, tc.stmt); return err })
		wantCode(t, tc.stmt, err, tc.want)
	}
}

// newExperiment takes tenant and slug; the title is the slug.
func newExperiment(t *testing.T, tx pgx.Tx, tenant, slug string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `INSERT INTO experiments (tenant_id, slug, title, created_by)
		VALUES ($1, $2, $2, 'it@lab') RETURNING id`, tenant, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertProtocolVersion takes tenant, experiment, version and protocol text.
// It creates the protocol's lineage node and returns the version and node ids.
func insertProtocolVersion(tx pgx.Tx, tenant, expID string, version int, md string) (id, nodeID string, err error) {
	ctx := context.Background()
	if err := tx.QueryRow(ctx, insertNode, tenant, "protocol").Scan(&nodeID); err != nil {
		return "", "", err
	}
	err = tx.QueryRow(ctx, `INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, node_id, created_by)
		VALUES ($1, $2, $3, $4, $5, 'it@lab') RETURNING id`, tenant, expID, version, md, nodeID).Scan(&id)
	return id, nodeID, err
}

func newProtocolVersion(t *testing.T, tx pgx.Tx, tenant, expID string, version int, md string) (id, nodeID string) {
	t.Helper()
	id, nodeID, err := insertProtocolVersion(tx, tenant, expID, version, md)
	if err != nil {
		t.Fatal(err)
	}
	return id, nodeID
}

// insertRun takes tenant, experiment and the pinned protocol version.
func insertRun(tx pgx.Tx, tenant, expID, protocolVersionID string) (runID string, err error) {
	ctx := context.Background()
	var nodeID string
	if err := tx.QueryRow(ctx, insertNode, tenant, "run").Scan(&nodeID); err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, `INSERT INTO runs (tenant_id, experiment_id, node_id, protocol_version_id, created_by)
		VALUES ($1, $2, $3, $4, 'it@lab') RETURNING id`, tenant, expID, nodeID, protocolVersionID).Scan(&runID)
	return runID, err
}

// SPEC-003-R01
func TestProtocolVersionHash(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var id string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		exp := newExperiment(t, tx, tenant, "hash")
		id, _ = newProtocolVersion(t, tx, tenant, exp, 1, "# A\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT sha256 FROM protocol_versions WHERE id = $1`, id).Scan(&got)
	})
	want := sha256.Sum256([]byte("# A\n"))
	if err != nil || !bytes.Equal(got, want[:]) {
		t.Fatalf("sha256 = %x, want %x (err %v)", got, want, err)
	}
	// The hash is computed by the database, so a supplied value is refused.
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		exp := newExperiment(t, tx, tenant, "hash-forged")
		var node string
		if err := tx.QueryRow(ctx, insertNode, tenant, "protocol").Scan(&node); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, node_id, created_by, sha256)
			VALUES ($1, $2, 1, 'x', $3, 'it@lab', '\x00'::bytea)`, tenant, exp, node)
		return err
	})
	wantCode(t, "INSERT with an explicit sha256", err, "428C9") // generated_always
}

// SPEC-003-R02
func TestProtocolVersionsAreAppendOnly(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		exp := newExperiment(t, tx, tenant, "append")
		newProtocolVersion(t, tx, tenant, exp, 1, "# v1")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE protocol_versions SET protocol_md = 'x' WHERE tenant_id = $1`,
		`DELETE FROM protocol_versions WHERE tenant_id = $1`,
	} {
		// A failing statement aborts its transaction, so each gets a fresh one.
		err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, tenant); return err })
		wantCode(t, "app role: "+stmt, err, "42501") // no grant for lde_app
		// Even the owner, bypassing WithTenant, is stopped by the triggers.
		_, err = db.Pool.Exec(ctx, stmt, tenant)
		wantCode(t, "owner: "+stmt, err, storage.CodeAppendOnly)
	}
	_, err = db.Pool.Exec(ctx, `TRUNCATE protocol_versions, runs`) // runs references it, so a bare TRUNCATE hits 0A000 first
	wantCode(t, "owner: TRUNCATE", err, storage.CodeAppendOnly)
}

// SPEC-003-R03
func TestProtocolVersionsAreGapless(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var e, f string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		e = newExperiment(t, tx, tenant, "gapless-e")
		newProtocolVersion(t, tx, tenant, e, 1, "v1")
		newProtocolVersion(t, tx, tenant, e, 2, "v2")
		f = newExperiment(t, tx, tenant, "gapless-f")
		newProtocolVersion(t, tx, tenant, f, 1, "v1")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, _, err := insertProtocolVersion(tx, tenant, e, 4, "v4")
		return err
	})
	wantCode(t, "version 4 after 2 skips 3", err, "23514")
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, _, err := insertProtocolVersion(tx, tenant, e, 3, "v3")
		return err
	})
	wantCode(t, "version 3 after 2", err, "")
}

// SPEC-003-R04
func TestRunProtocolMustBelongToExperiment(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var e, pe1, pf1 string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		e = newExperiment(t, tx, tenant, "pin-e")
		pe1, _ = newProtocolVersion(t, tx, tenant, e, 1, "pe1")
		f := newExperiment(t, tx, tenant, "pin-f")
		pf1, _ = newProtocolVersion(t, tx, tenant, f, 1, "pf1")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, err := insertRun(tx, tenant, e, pf1)
		return err
	})
	wantCode(t, "run of E pinned to protocol of F", err, "23503")
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, err := insertRun(tx, tenant, e, pe1)
		return err
	})
	wantCode(t, "run of E pinned to protocol of E", err, "")
}

// SPEC-003-R11
func TestRunProtocolPinIsImmutable(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var runID, pe2 string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		e := newExperiment(t, tx, tenant, "immutable")
		pe1, _ := newProtocolVersion(t, tx, tenant, e, 1, "v1")
		pe2, _ = newProtocolVersion(t, tx, tenant, e, 2, "v2")
		var err error
		runID, err = insertRun(tx, tenant, e, pe1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	const repin = `UPDATE runs SET protocol_version_id = $2 WHERE id = $1`
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, repin, runID, pe2)
		return err
	})
	wantCode(t, "app role re-pins planned run", err, storage.CodeAppendOnly)
	_, err = db.Pool.Exec(ctx, repin, runID, pe2)
	wantCode(t, "owner re-pins planned run", err, storage.CodeAppendOnly)
	_, err = db.Pool.Exec(ctx, `UPDATE runs SET state = 'running' WHERE id = $1`, runID)
	wantCode(t, "owner starts run without touching the pin", err, "")
}

// insertPrincipal takes tenant, kind and handle; the subject is the handle.
const insertPrincipal = `INSERT INTO principals (tenant_id, kind, handle, issuer, subject)
	VALUES ($1, $2, $3, 'https://idp.test', $3) RETURNING id`

// SPEC-006-R04
func TestPrincipalHandlesUniqueAndImmutable(t *testing.T) {
	db := openTestDB(t)
	t1, t2 := newTenant(t, db), newTenant(t, db)
	ctx := context.Background()
	alice := newPrincipal(t, db, t1, "alice", "human").ID
	_, err := db.Pool.Exec(ctx, `INSERT INTO principals (tenant_id, kind, handle, issuer, subject)
		VALUES ($1, 'human', 'alice', 'https://idp.test', 'alice-2')`, t1)
	wantCode(t, "second alice in T1", err, "23505")
	newPrincipal(t, db, t2, "alice", "human") // fails the test if T2 rejects alice
	for _, stmt := range []string{
		`UPDATE principals SET handle = 'al' WHERE id = $1`,
		`UPDATE principals SET subject = 'x' WHERE id = $1`,
		`DELETE FROM principals WHERE id = $1`,
	} {
		_, err := db.Pool.Exec(ctx, stmt, alice)
		wantCode(t, stmt, err, storage.CodeAppendOnly)
	}
}

// SPEC-006-R05
func TestRoleGrantsAdminOnlyAndAudited(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	alice := newPrincipal(t, db, tenant, "alice", "human", "scientist")
	carol := newPrincipal(t, db, tenant, "carol", "human", "admin")

	err := db.WithTenant(ctx, tenant, alice, func(tx pgx.Tx) error {
		_, err := grantRole(ctx, tx, tenant, "alice", alice.ID, alice.ID, "reviewer", "self", true)
		return err
	})
	wantCode(t, "alice grants herself reviewer", err, storage.CodeRoleDenied)

	var grant string
	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) (err error) {
		grant, err = grantRole(ctx, tx, tenant, "carol", alice.ID, alice.ID, "reviewer", "cover for bob", true)
		return err
	})
	wantCode(t, "carol grants alice reviewer", err, "")
	var grantedBy, action, reason string
	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT g.granted_by::text, a.action, a.reason FROM role_grants g
			JOIN audit_events a ON a.entity_type = 'role_grant' AND a.entity_id = g.id::text
			WHERE g.id = $1`, grant).Scan(&grantedBy, &action, &reason)
	})
	if err != nil || grantedBy != carol.ID || action != "role.granted" || reason != "cover for bob" {
		t.Errorf("grant: granted_by %s (carol %s), event %q %q, err %v", grantedBy, carol.ID, action, reason, err)
	}

	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		_, err := grantRole(ctx, tx, tenant, "carol", alice.ID, carol.ID, "qa", "no event", false)
		return err
	})
	wantCode(t, "grant with no audit event", err, storage.CodeAuditLink)
	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		_, err := grantRole(ctx, tx, tenant, "carol", alice.ID, carol.ID, "qa", " ", true)
		return err
	})
	wantCode(t, "grant with blank reason", err, "23514")

	revoke := func() error {
		return db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE role_grants SET revoked_by = $2, revoked_at = now(), revoke_reason = 'bob is back'
				WHERE id = $1`, grant, alice.ID); err != nil {
				return err
			}
			_, err := audit.Append(ctx, tx, tenant, audit.Entry{
				Actor: "carol", Action: "role.revoked", EntityType: "role_grant", EntityID: grant, Reason: "bob is back",
			})
			return err
		})
	}
	wantCode(t, "carol revokes", revoke(), "")
	var revokedBy string
	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT revoked_by::text FROM role_grants WHERE id = $1`, grant).Scan(&revokedBy)
	})
	if err != nil || revokedBy != carol.ID {
		t.Errorf("revoked_by %s, want carol %s (err %v)", revokedBy, carol.ID, err)
	}
	wantCode(t, "carol revokes again", revoke(), storage.CodeAppendOnly)
	_, err = db.Pool.Exec(ctx, `DELETE FROM role_grants WHERE id = $1`, grant)
	wantCode(t, "owner deletes grant", err, storage.CodeAppendOnly)
}

// delegate inserts a delegation from human to agent, with its
// delegation.created event, as whoever tx acts as.
func delegate(ctx context.Context, tx pgx.Tx, tenant, actor, agentID, humanID, createdBy string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO agent_delegations (tenant_id, agent_id, human_id, reason, created_by)
		VALUES ($1, $2, $3, 'fixture', $4) RETURNING id`, tenant, agentID, humanID, createdBy).Scan(&id); err != nil {
		return "", err
	}
	_, err := audit.Append(ctx, tx, tenant, audit.Entry{
		Actor: actor, Action: "delegation.created", EntityType: "agent_delegation", EntityID: id, Reason: "fixture",
	})
	return id, err
}

// SPEC-006-R08
func TestAgentRolesIntersectHuman(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	alice := newPrincipal(t, db, tenant, "alice", "human", "scientist")
	carol := newPrincipal(t, db, tenant, "carol", "human", "admin")
	bot := newPrincipal(t, db, tenant, "bot", "agent", "scientist", "reviewer")
	forAlice := storage.Principal{ID: bot.ID, OnBehalfOf: alice.ID}

	holds := func(what string, as storage.Principal, role string, want bool) {
		t.Helper()
		var got bool
		err := db.WithTenant(ctx, tenant, as, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT lde_has_role($1)`, role).Scan(&got)
		})
		if err != nil || got != want {
			t.Errorf("%s holds %s = %v, want %v (err %v)", what, role, got, want, err)
		}
	}
	holds("bot for alice, no delegation", forAlice, "scientist", false)
	holds("bot for alice, no delegation", forAlice, "reviewer", false)

	var id string
	err := db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) (err error) {
		id, err = delegate(ctx, tx, tenant, "carol", bot.ID, alice.ID, carol.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	holds("bot for alice", forAlice, "scientist", true)
	holds("bot for alice", forAlice, "reviewer", false)
	holds("bot for carol", storage.Principal{ID: bot.ID, OnBehalfOf: carol.ID}, "scientist", false)
	holds("bot for nobody", storage.Principal{ID: bot.ID}, "scientist", false)

	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE agent_delegations SET ended_by = $2, ended_at = now() WHERE id = $1`,
			id, carol.ID); err != nil {
			return err
		}
		_, err := audit.Append(ctx, tx, tenant, audit.Entry{
			Actor: "carol", Action: "delegation.ended", EntityType: "agent_delegation", EntityID: id,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	holds("bot for alice, delegation ended", forAlice, "scientist", false)

	err = db.WithTenant(ctx, tenant, alice, func(tx pgx.Tx) error {
		_, err := delegate(ctx, tx, tenant, "alice", bot.ID, alice.ID, alice.ID)
		return err
	})
	wantCode(t, "alice delegates", err, storage.CodeRoleDenied)
	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		_, err := delegate(ctx, tx, tenant, "carol", carol.ID, alice.ID, carol.ID)
		return err
	})
	wantCode(t, "delegation to a human", err, "23514")
}

// SPEC-006-R06, SPEC-006-R08
func TestRoleGatesOnLabRecords(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	alice := newPrincipal(t, db, tenant, "alice", "human", "scientist")
	bob := newPrincipal(t, db, tenant, "bob", "human", "reviewer")
	carol := newPrincipal(t, db, tenant, "carol", "human", "admin")
	bot := newPrincipal(t, db, tenant, "bot", "agent", "scientist", "reviewer")

	// Alice, a scientist, records an experiment with two completed runs.
	var run1, run2 string
	err := db.WithTenant(ctx, tenant, alice, func(tx pgx.Tx) error {
		exp := newExperiment(t, tx, tenant, "gates")
		pv, _ := newProtocolVersion(t, tx, tenant, exp, 1, "# p\n")
		var err error
		if run1, err = insertRun(tx, tenant, exp, pv); err != nil {
			return err
		}
		if run2, err = insertRun(tx, tenant, exp, pv); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE runs SET state = 'completed'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	sign := func(as storage.Principal, run string) error {
		return db.WithTenant(ctx, tenant, as, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE runs SET state = 'signed' WHERE id = $1`, run)
			return err
		})
	}
	addNode := func(as storage.Principal) error {
		return db.WithTenant(ctx, tenant, as, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, insertNode, tenant, "sample")
			return err
		})
	}
	wantCode(t, "alice (scientist) signs", sign(alice, run1), storage.CodeRoleDenied)
	wantCode(t, "bob (reviewer) signs", sign(bob, run1), "")
	wantCode(t, "bob (reviewer) creates a node", addNode(bob), storage.CodeRoleDenied)
	wantCode(t, "nobody creates a node", addNode(storage.Principal{}), storage.CodeRoleDenied)

	err = db.WithTenant(ctx, tenant, carol, func(tx pgx.Tx) error {
		_, err := delegate(ctx, tx, tenant, "carol", bot.ID, alice.ID, carol.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	botForAlice := storage.Principal{ID: bot.ID, OnBehalfOf: alice.ID}
	wantCode(t, "bot for alice creates a node", addNode(botForAlice), "")
	wantCode(t, "bot for alice signs", sign(botForAlice, run2), storage.CodeRoleDenied)
}

// SPEC-006-R06: signing is the only change a reviewer may make.
func TestSigningChangesNothingElse(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	bob := newPrincipal(t, db, tenant, "bob", "human", "reviewer")

	runs := make([]string, 3)
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		exp := newExperiment(t, tx, tenant, "sign-only")
		pv, _ := newProtocolVersion(t, tx, tenant, exp, 1, "# p\n")
		for i := range runs {
			var err error
			if runs[i], err = insertRun(tx, tenant, exp, pv); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE runs SET state = 'completed'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	exec := func(as storage.Principal, stmt, run string) error {
		return db.WithTenant(ctx, tenant, as, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, stmt, run)
			return err
		})
	}
	const signAndEdit = `UPDATE runs SET state = 'signed', params = '{"tampered": true}' WHERE id = $1`
	const signOnly = `UPDATE runs SET state = 'signed', started_at = started_at, finished_at = finished_at WHERE id = $1`
	wantCode(t, "bob (reviewer) signs and edits params", exec(bob, signAndEdit, runs[0]), storage.CodeRoleDenied)
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, tenant, "it@lab", "correct params at sign-off"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, signAndEdit, runs[1])
		return err
	})
	wantCode(t, "it@lab (scientist and reviewer) signs and edits params", err, "")
	wantCode(t, "bob (reviewer) signs, rewriting unchanged columns", exec(bob, signOnly, runs[2]), "")
}

// commitChangeSet takes tenant, author and reason. It inserts a change set
// that is committed in tx, then names it as the transaction's change set.
func commitChangeSet(ctx context.Context, tx pgx.Tx, tenant, author, reason string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO change_sets (tenant_id, title, reason, author, state, items, decision_reason)
		VALUES ($1, 'edit', $2, $3, 'committed', '[]', $2) RETURNING id`, tenant, reason, author).Scan(&id); err != nil {
		return "", err
	}
	_, err := tx.Exec(ctx, `SELECT set_config('app.change_set_id', $1, true)`, id)
	return id, err
}

// proposeChangeSet takes tenant and author. It inserts a proposed change set.
func proposeChangeSet(ctx context.Context, tx pgx.Tx, tenant, author string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO change_sets (tenant_id, title, reason, author, state, items)
		VALUES ($1, 'proposal', 'fixture', $2, 'proposed', '[]') RETURNING id`, tenant, author).Scan(&id)
	return id, err
}

// SPEC-007-R06
func TestAgentsCannotDecideChangeSets(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	alice := newPrincipal(t, db, tenant, "alice", "human", "scientist")
	bot := newPrincipal(t, db, tenant, "bot", "agent", "scientist", "reviewer")
	svc := newPrincipal(t, db, tenant, "svc", "service", "scientist")
	botForAlice := storage.Principal{ID: bot.ID, OnBehalfOf: alice.ID}
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, err := delegate(ctx, tx, tenant, "it@lab", bot.ID, alice.ID, as(tenant).ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// update sets a change set's state and decision reason as p.
	update := func(p storage.Principal, id, state string) error {
		return db.WithTenant(ctx, tenant, p, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE change_sets SET state = $2, decision_reason = 'ok' WHERE id = $1`, id, state)
			return err
		})
	}

	var cs1, cs2 string
	err = db.WithTenant(ctx, tenant, botForAlice, func(tx pgx.Tx) (err error) {
		cs1, err = proposeChangeSet(ctx, tx, tenant, "alice via bot")
		return err
	})
	wantCode(t, "bot for alice proposes", err, "")
	wantCode(t, "bot for alice commits its proposal", update(botForAlice, cs1, "committed"), "LDE05")
	wantCode(t, "bot for alice rejects its proposal", update(botForAlice, cs1, "rejected"), "LDE05")
	err = db.WithTenant(ctx, tenant, botForAlice, func(tx pgx.Tx) error {
		_, err := commitChangeSet(ctx, tx, tenant, "alice via bot", "direct edit")
		return err
	})
	wantCode(t, "bot for alice inserts a committed change set", err, "LDE05")

	err = db.WithTenant(ctx, tenant, svc, func(tx pgx.Tx) (err error) {
		cs2, err = proposeChangeSet(ctx, tx, tenant, "svc")
		return err
	})
	wantCode(t, "service proposes", err, "")
	wantCode(t, "service commits", update(svc, cs2, "committed"), "LDE05")

	wantCode(t, "alice commits her proposal", update(alice, cs1, "committed"), "")
}

// SPEC-007-R01
func TestRevisionsKeepPriorValues(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var s string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO lineage_nodes (tenant_id, kind, label, attrs, created_by)
			VALUES ($1, 'sample', 'Plasma 042', '{"volume_ul": 50}', 'alice') RETURNING id`, tenant).Scan(&s)
	})
	if err != nil {
		t.Fatal(err)
	}
	var cs string
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) (err error) {
		if cs, err = commitChangeSet(ctx, tx, tenant, "alice", "pipetting error"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE lineage_nodes SET attrs = '{"volume_ul": 45}' WHERE id = $1`, s)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, tenant, "alice", "no-op"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE lineage_nodes SET label = label WHERE id = $1`, s)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	type revision struct {
		rev            int
		value          []byte
		author, reason string
		csID           string
	}
	var got []revision
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT rev, value, author, reason, COALESCE(change_set_id::text, '')
			FROM record_revisions WHERE entity_type = 'lineage_node' AND entity_id = $1 ORDER BY rev`, s)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r revision
			if err := rows.Scan(&r.rev, &r.value, &r.author, &r.reason, &r.csID); err != nil {
				return err
			}
			got = append(got, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []revision{
		{1, []byte(`{"label": "Plasma 042", "attrs": {"volume_ul": 50}}`), "alice", "created", ""},
		{2, []byte(`{"label": "Plasma 042", "attrs": {"volume_ul": 45}}`), "alice", "pipetting error", cs},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d revisions, want %d", len(got), len(want))
	}
	for i := range want {
		var gv, wv map[string]any
		if err := json.Unmarshal(got[i].value, &gv); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(want[i].value, &wv); err != nil {
			t.Fatal(err)
		}
		if got[i].rev != want[i].rev || !reflect.DeepEqual(gv, wv) || got[i].author != want[i].author ||
			got[i].reason != want[i].reason || got[i].csID != want[i].csID {
			t.Errorf("revision %d: got %+v (value %v), want %+v", i+1, got[i], gv, want[i])
		}
	}

	var attrs []byte
	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT attrs FROM lineage_nodes WHERE id = $1`, s).Scan(&attrs)
	})
	var current map[string]any
	if err == nil {
		err = json.Unmarshal(attrs, &current)
	}
	if err != nil || !reflect.DeepEqual(current, map[string]any{"volume_ul": float64(45)}) {
		t.Errorf("current attrs = %v (err %v), want volume_ul 45", current, err)
	}
}

// SPEC-007-R02
func TestRevisableFieldsNeedChangeSet(t *testing.T) {
	db := openTestDB(t)
	f := newSealFixture(t, db, "completed")
	ctx := context.Background()
	var exp string
	if err := db.Pool.QueryRow(ctx, `SELECT experiment_id FROM runs WHERE id = $1`, f.runID).Scan(&exp); err != nil {
		t.Fatal(err)
	}
	// inTx runs fn as the fixture's tenant, in its own transaction.
	inTx := func(fn func(tx pgx.Tx) error) error {
		return db.WithTenant(ctx, f.tenant, as(f.tenant), fn)
	}
	// exec runs one statement in its own transaction.
	exec := func(stmt string, args ...any) error {
		return inTx(func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, stmt, args...)
			return err
		})
	}
	// The sample's attrs default to '{}', so the edit must change them.
	const editNode = `UPDATE lineage_nodes SET attrs = '{"x": 1}' WHERE id = $1`

	wantCode(t, "no change set: node attrs", exec(editNode, f.s), storage.CodeAppendOnly)
	wantCode(t, "no change set: run params", exec(`UPDATE runs SET params = '{"x": 1}' WHERE id = $1`, f.runID), storage.CodeAppendOnly)
	wantCode(t, "no change set: experiment title", exec(`UPDATE experiments SET title = 'x' WHERE id = $1`, exp), storage.CodeAppendOnly)

	err := inTx(func(tx pgx.Tx) error {
		id, err := proposeChangeSet(ctx, tx, f.tenant, "it@lab")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.change_set_id', $1, true)`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, editNode, f.s)
		return err
	})
	wantCode(t, "proposed change set", err, storage.CodeAppendOnly)

	var cs string
	err = inTx(func(tx pgx.Tx) (err error) {
		cs, err = commitChangeSet(ctx, tx, f.tenant, "it@lab", "committed earlier")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = inTx(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.change_set_id', $1, true)`, cs); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, editNode, f.s)
		return err
	})
	wantCode(t, "change set committed in an earlier transaction", err, storage.CodeAppendOnly)

	err = inTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO change_sets (tenant_id, title, reason, author, state, items, decision_reason)
			VALUES ($1, 'blank', '  ', 'it@lab', 'committed', '[]', 'ok')`, f.tenant)
		return err
	})
	wantCode(t, "committed change set with a blank reason", err, "23514")

	wantCode(t, "state is not revisable", exec(`UPDATE runs SET state = 'running' WHERE id = $1`, f.runID), "")

	err = inTx(func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, f.tenant, "it@lab", "pipetting"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, editNode, f.s)
		return err
	})
	wantCode(t, "inside a committing change set", err, "")

	// A TEMP table named change_sets must not stand in for the real one.
	countRevs := func() (n int) {
		err := inTx(func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM public.record_revisions WHERE entity_id = $1`, f.s).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := countRevs()
	err = inTx(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE change_sets (LIKE public.change_sets INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
			t.Skipf("lde_app cannot create temp tables: %v", err)
		}
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE record_revisions (LIKE public.record_revisions) ON COMMIT DROP`); err != nil {
			return err
		}
		var forged string
		err := tx.QueryRow(ctx, `INSERT INTO pg_temp.change_sets (id, tenant_id, title, reason, author, state, items, decision_reason)
			VALUES (gen_random_uuid(), $1, 'forged', 'forged', 'it@lab', 'committed', '[]', 'ok') RETURNING id`, f.tenant).Scan(&forged)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.change_set_id', $1, true)`, forged); err != nil {
			return err
		}
		// The committing case above already set {"x": 1}; edit to a new value.
		_, err = tx.Exec(ctx, `UPDATE lineage_nodes SET attrs = '{"x": 2}' WHERE id = $1`, f.s)
		return err
	})
	wantCode(t, "forged change set in a temp table", err, storage.CodeAppendOnly)
	if after := countRevs(); after != before {
		t.Errorf("revisions of node: %d before, %d after the forged edit", before, after)
	}

	// TEMP tables named runs and lineage_edges must not switch off the
	// sealed-params check (SPEC-007-R10).
	sf := newSealFixture(t, db, "signed")
	err = db.WithTenant(ctx, sf.tenant, as(sf.tenant), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE runs (tenant_id uuid, node_id uuid, state text) ON COMMIT DROP`); err != nil {
			t.Skipf("lde_app cannot create temp tables: %v", err)
		}
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE lineage_edges (tenant_id uuid, from_id uuid, to_id uuid, relation text) ON COMMIT DROP`); err != nil {
			return err
		}
		if _, err := commitChangeSet(ctx, tx, sf.tenant, "it@lab", "shadowed params"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE public.runs SET params = '{"x": 1}' WHERE id = $1`, sf.runID)
		return err
	})
	wantCode(t, "sealed run params with shadow tables", err, storage.CodeSealed)
}

// SPEC-007-R03
func TestRevisionsAreAppendOnly(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	ctx := context.Background()
	var node string
	err := db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, insertNode, tenant, "sample").Scan(&node)
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Pool.Exec(ctx, `UPDATE record_revisions SET reason = 'x' WHERE tenant_id = $1`, tenant)
	wantCode(t, "owner updates a revision", err, storage.CodeAppendOnly)
	_, err = db.Pool.Exec(ctx, `DELETE FROM record_revisions WHERE tenant_id = $1`, tenant)
	wantCode(t, "owner deletes a revision", err, storage.CodeAppendOnly)
	_, err = db.Pool.Exec(ctx, `TRUNCATE record_revisions`)
	wantCode(t, "owner truncates revisions", err, storage.CodeAppendOnly)

	err = db.WithTenant(ctx, tenant, as(tenant), func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason)
			VALUES ($1, 'lineage_node', $2, 9, '{}', 'x', 'x')`, tenant, node)
		return err
	})
	wantCode(t, "app role inserts a revision", err, "42501")
}

// SPEC-007-R13
func TestNotesOnSealedNodes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	f := newSealFixture(t, db, "completed")
	const insertNote = `INSERT INTO notes (tenant_id, node_id, body, created_by)
		VALUES ($1, $2, $3, 'alice') RETURNING id, annotation`

	var n1, n2 string
	var annotation bool
	err := db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, insertNote, f.tenant, f.r, "Thawed at 09:00").Scan(&n1, &annotation)
	})
	if err != nil {
		t.Fatal(err)
	}
	if annotation {
		t.Error("note before signing: want annotation = false")
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE runs SET state = 'signed' WHERE id = $1`, f.runID); err != nil {
		t.Fatal(err)
	}

	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, insertNote, f.tenant, f.r, "Freezer alarm at 02:10, samples unaffected").Scan(&n2, &annotation)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !annotation {
		t.Error("note on signed run: want annotation = true")
	}
	var sealed bool
	if err := db.Pool.QueryRow(ctx, `SELECT lineage_node_sealed($1)`, f.r).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if !sealed {
		t.Error("annotation broke the seal: want lineage_node_sealed = true")
	}

	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, f.tenant, "it@lab", "correct thaw time"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notes SET body = 'Thawed at 09:15' WHERE id = $1`, n1)
		return err
	})
	wantCode(t, "edit note written before signing", err, storage.CodeSealed)

	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		if _, err := commitChangeSet(ctx, tx, f.tenant, "it@lab", "annotate sample check"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notes SET body = 'Freezer alarm at 02:10; checked, samples unaffected' WHERE id = $1`, n2)
		return err
	})
	wantCode(t, "edit annotation", err, "")

	_, err = db.Pool.Exec(ctx, `UPDATE notes SET annotation = true WHERE id = $1`, n1)
	wantCode(t, "owner sets annotation on a sealed-era note", err, storage.CodeAppendOnly)

	// TEMP tables named runs and lineage_edges must not shadow the real ones.
	err = db.WithTenant(ctx, f.tenant, as(f.tenant), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE runs (tenant_id uuid, node_id uuid, state text) ON COMMIT DROP`); err != nil {
			t.Skipf("lde_app cannot create temp tables: %v", err)
		}
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE lineage_edges (tenant_id uuid, from_id uuid, to_id uuid, relation text) ON COMMIT DROP`); err != nil {
			return err
		}
		var shadowed bool
		if err := tx.QueryRow(ctx, insertNote, f.tenant, f.r, "Shadowed insert").Scan(new(string), &shadowed); err != nil {
			return err
		}
		if !shadowed {
			t.Error("note on signed run with shadow tables: want annotation = true")
		}
		if _, err := commitChangeSet(ctx, tx, f.tenant, "it@lab", "shadowed edit"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notes SET body = 'Thawed at 09:30' WHERE id = $1`, n1)
		return err
	})
	wantCode(t, "edit frozen note with shadow tables", err, storage.CodeSealed)
}
