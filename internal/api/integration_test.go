package api_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/api"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/identity"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/storage"
)

// These tests drive the real handler against the database, for rules the API
// enforces rather than the database. Run them with: make test-integration.
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

// call sends body as JSON (when non-nil) and decodes the response into out.
func call(t *testing.T, srv *httptest.Server, tenant, method, path string, body, out any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-LDE-Tenant", tenant)
	req.Header.Set("X-LDE-Actor", "it@lab")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
}

// SPEC-003-R06, SPEC-003-R07
func TestRunPinsProtocolVersion(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = (b[6]&0x0f)|0x40, (b[8]&0x3f)|0x80
	tenant := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	if err := db.EnsureTenant(ctx, tenant, "t-"+tenant[:8], "test"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(config.Config{Env: "dev"}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).Handler())
	defer srv.Close()

	var exp struct {
		ID                string `json:"id"`
		ProtocolVersionID string `json:"protocol_version_id"`
		ProtocolSHA256    string `json:"protocol_sha256"`
	}
	call(t, srv, tenant, "POST", "/v1/experiments", map[string]string{"slug": "e", "title": "E", "protocol_md": "# A\n"}, &exp)

	var run struct {
		ID                string `json:"id"`
		NodeID            string `json:"node_id"`
		ProtocolVersionID string `json:"protocol_version_id"`
	}
	call(t, srv, tenant, "POST", "/v1/runs", map[string]string{"experiment_id": exp.ID}, &run)
	if run.ProtocolVersionID != exp.ProtocolVersionID {
		t.Fatalf("run pinned %q, want highest version %q", run.ProtocolVersionID, exp.ProtocolVersionID)
	}

	// R06: the version's node is upstream of the run, linked by used.
	var pv struct {
		NodeID string `json:"node_id"`
	}
	call(t, srv, tenant, "GET", "/v1/protocol-versions/"+exp.ProtocolVersionID, nil, &pv)
	var lin struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Edges []struct {
			From, To, Relation string
		} `json:"edges"`
	}
	call(t, srv, tenant, "GET", "/v1/lineage/"+run.NodeID, nil, &lin)
	var hasNode, hasEdge bool
	for _, n := range lin.Nodes {
		hasNode = hasNode || n.ID == pv.NodeID
	}
	for _, e := range lin.Edges {
		hasEdge = hasEdge || (e.From == pv.NodeID && e.To == run.NodeID && e.Relation == "used")
	}
	if !hasNode || !hasEdge {
		t.Errorf("upstream lineage of run: protocol node %v, used edge %v; got %+v", hasNode, hasEdge, lin)
	}

	// R07: run.created records the pinned version and its hash.
	var payload struct {
		ProtocolVersionID string `json:"protocol_version_id"`
		ProtocolVersion   int    `json:"protocol_version"`
		ProtocolSHA256    string `json:"protocol_sha256"`
	}
	err := db.WithTenant(ctx, tenant, storage.Principal{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload_json FROM audit_events WHERE action = 'run.created' AND entity_id = $1`,
			run.ID).Scan(&payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.ProtocolVersionID != exp.ProtocolVersionID || payload.ProtocolVersion != 1 || payload.ProtocolSHA256 != exp.ProtocolSHA256 {
		t.Errorf("run.created payload = %+v, want version %s (1) with sha256 %s", payload, exp.ProtocolVersionID, exp.ProtocolSHA256)
	}
}

// newTenant creates an empty tenant with a random id.
func newTenant(t *testing.T, db *storage.DB) string {
	t.Helper()
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = (b[6]&0x0f)|0x40, (b[8]&0x3f)|0x80
	tenant := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	if err := db.EnsureTenant(context.Background(), tenant, "t-"+tenant[:8], "test"); err != nil {
		t.Fatal(err)
	}
	return tenant
}

const testIssuer = "https://idp.test"

// mintToken signs claims as an RS256 JWT.
func mintToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signing := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// claims are valid for five minutes, for audience labrat.
func claims(tenant, subject string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": testIssuer, "aud": "labrat", "sub": subject, "lde_tenant": tenant,
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// prodServer serves the API in prod, trusting tokens that key signed.
func prodServer(t *testing.T, db *storage.DB, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	cfg := config.Config{Env: "prod", OIDCIssuer: testIssuer, OIDCAudience: "labrat", OIDCTenantClaim: "lde_tenant"}
	verifier := oidc.NewVerifier(testIssuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
		&oidc.Config{ClientID: "labrat"})
	srv := httptest.NewServer(api.New(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)), verifier).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// send makes a request with an optional bearer token and extra headers, and
// returns the status and body.
func send(t *testing.T, srv *httptest.Server, token string, headers map[string]string, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// fixture is SPEC-006's tenant T1: alice (scientist), bob (reviewer), carol
// (admin) and bot (agent; scientist, reviewer), all at testIssuer with
// subject = handle.
type fixture struct {
	tenant                 string
	alice, bob, carol, bot identity.Principal
}

func newFixture(t *testing.T, db *storage.DB) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{tenant: newTenant(t, db)}
	err := db.WithOwner(ctx, func(tx pgx.Tx) error {
		for _, p := range []struct {
			out          *identity.Principal
			handle, kind string
			roles        []string
		}{
			{&f.alice, "alice", "human", []string{"scientist"}},
			{&f.bob, "bob", "human", []string{"reviewer"}},
			{&f.carol, "carol", "human", []string{"admin"}},
			{&f.bot, "bot", "agent", []string{"scientist", "reviewer"}},
		} {
			var err error
			*p.out, err = identity.Create(ctx, tx, "system:test", "fixture", identity.Principal{
				TenantID: f.tenant, Kind: p.kind, Handle: p.handle, Issuer: testIssuer, Subject: p.handle,
			})
			if err != nil {
				return err
			}
			for _, role := range p.roles {
				if _, err := identity.Grant(ctx, tx, "system:test", f.tenant, p.out.ID, role, "", "fixture", p.out.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// auditActors lists the actors of a tenant's events about one entity.
func auditActors(t *testing.T, db *storage.DB, tenant, entityID string) []string {
	t.Helper()
	ctx := context.Background()
	var actors []string
	err := db.WithTenant(ctx, tenant, storage.Principal{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT actor FROM audit_events WHERE entity_id = $1 ORDER BY seq`, entityID)
		if err != nil {
			return err
		}
		actors, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return actors
}

func auditCount(t *testing.T, db *storage.DB, tenant string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := db.WithTenant(ctx, tenant, storage.Principal{}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var sampleNode = map[string]string{"kind": "sample", "label": "S1"}

// SPEC-006-R01
func TestBearerTokenRequired(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)

	expired := claims(f.tenant, "alice")
	expired["exp"] = time.Now().Add(-time.Hour).Unix()
	wrongAudience := claims(f.tenant, "alice")
	wrongAudience["aud"] = "someone-else"

	before := auditCount(t, db, f.tenant)
	for name, token := range map[string]string{
		"no token":       "",
		"another key":    mintToken(t, newKey(t), claims(f.tenant, "alice")),
		"expired":        mintToken(t, key, expired),
		"wrong audience": mintToken(t, key, wrongAudience),
	} {
		status, body := send(t, srv, token, nil, "POST", "/v1/lineage/nodes", sampleNode)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: status %d %s, want 401", name, status, body)
		}
	}
	if after := auditCount(t, db, f.tenant); after != before {
		t.Errorf("audit chain grew from %d to %d events", before, after)
	}
}

// SPEC-006-R02
func TestTenantFromTokenOnly(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	t2 := newTenant(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)

	status, body := send(t, srv, mintToken(t, key, claims(f.tenant, "alice")),
		map[string]string{"X-LDE-Tenant": t2, "X-LDE-Actor": "mallory"}, "POST", "/v1/lineage/nodes", sampleNode)
	if status != http.StatusCreated {
		t.Fatalf("create node: %d %s", status, body)
	}
	var node struct{ ID string }
	if err := json.Unmarshal(body, &node); err != nil {
		t.Fatal(err)
	}
	if got := auditActors(t, db, f.tenant, node.ID); len(got) != 1 || got[0] != "alice" {
		t.Errorf("T1 events for the node: actors %v, want [alice]", got)
	}
	if got := auditActors(t, db, t2, node.ID); len(got) != 0 {
		t.Errorf("T2 events for the node: actors %v, want none", got)
	}
}

// SPEC-006-R03
func TestUnknownOrDisabledPrincipal(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)
	ctx := context.Background()

	if status, body := send(t, srv, mintToken(t, key, claims(f.tenant, "zed")), nil, "GET", "/v1/experiments", nil); status != http.StatusForbidden {
		t.Errorf("unknown subject zed: %d %s, want 403", status, body)
	}
	alice := mintToken(t, key, claims(f.tenant, "alice"))
	if status, body := send(t, srv, alice, nil, "GET", "/v1/experiments", nil); status != http.StatusOK {
		t.Fatalf("alice before disabling: %d %s", status, body)
	}
	err := db.WithOwner(ctx, func(tx pgx.Tx) error {
		_, err := identity.SetEnabled(ctx, tx, "system:test", f.tenant, f.alice.ID, false, "left the lab")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, body := send(t, srv, alice, nil, "GET", "/v1/experiments", nil); status != http.StatusForbidden {
		t.Errorf("disabled alice: %d %s, want 403", status, body)
	}
}

// delegateAliceToBot has carol delegate alice to bot, as the owner.
func delegateAliceToBot(t *testing.T, db *storage.DB, f fixture) {
	t.Helper()
	ctx := context.Background()
	err := db.WithOwner(ctx, func(tx pgx.Tx) error {
		_, err := identity.Delegate(ctx, tx, "carol", f.tenant, f.bot.ID, f.alice.ID, "fixture", f.carol.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

var forAlice = map[string]string{"X-LDE-On-Behalf-Of": "alice"}

// SPEC-006-R07
func TestAgentNeedsDelegation(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)
	bot := mintToken(t, key, claims(f.tenant, "bot"))

	check := func(what, token string, headers map[string]string, want int) {
		t.Helper()
		if status, body := send(t, srv, token, headers, "GET", "/v1/experiments", nil); status != want {
			t.Errorf("%s: %d %s, want %d", what, status, body, want)
		}
	}
	check("bot, no delegation, no header", bot, nil, http.StatusForbidden)
	check("bot, no delegation, for alice", bot, forAlice, http.StatusForbidden)
	delegateAliceToBot(t, db, f)
	check("bot, delegated, for alice", bot, forAlice, http.StatusOK)
	check("bot, delegated, no header", bot, nil, http.StatusForbidden)
	check("alice naming someone to act for", mintToken(t, key, claims(f.tenant, "alice")),
		map[string]string{"X-LDE-On-Behalf-Of": "bob"}, http.StatusBadRequest)
}

// SPEC-006-R09
func TestAgentActorIsHumanViaAgent(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)
	delegateAliceToBot(t, db, f)

	status, body := send(t, srv, mintToken(t, key, claims(f.tenant, "bot")), forAlice, "POST", "/v1/lineage/nodes", sampleNode)
	if status != http.StatusCreated {
		t.Fatalf("bot for alice creates a node: %d %s", status, body)
	}
	var node struct{ ID string }
	if err := json.Unmarshal(body, &node); err != nil {
		t.Fatal(err)
	}
	if got := auditActors(t, db, f.tenant, node.ID); len(got) != 1 || got[0] != "alice via bot" {
		t.Errorf("node events: actors %v, want [alice via bot]", got)
	}
}

// sendWant sends a request, fails the test unless the status is want, and
// decodes the response into out when out is non-nil.
func sendWant(t *testing.T, srv *httptest.Server, token string, headers map[string]string, want int, method, path string, body, out any) {
	t.Helper()
	status, raw := send(t, srv, token, headers, method, path, body)
	if status != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, status, raw, want)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// createNode creates a lineage node and returns its id.
func createNode(t *testing.T, srv *httptest.Server, token string, headers map[string]string, body map[string]any) string {
	t.Helper()
	var n struct {
		ID string `json:"id"`
	}
	sendWant(t, srv, token, headers, http.StatusCreated, "POST", "/v1/lineage/nodes", body, &n)
	return n.ID
}

// SPEC-007-R05
func TestProposedChangesInvisible(t *testing.T) {
	db := openTestDB(t)
	f := newFixture(t, db)
	key := newKey(t)
	srv := prodServer(t, db, key)
	delegateAliceToBot(t, db, f)
	alice := mintToken(t, key, claims(f.tenant, "alice"))
	bot := mintToken(t, key, claims(f.tenant, "bot"))
	ctx := context.Background()

	s := createNode(t, srv, alice, nil, map[string]any{"kind": "sample", "label": "Plasma 042", "attrs": map[string]any{"volume_ul": 50}})
	r := createNode(t, srv, alice, nil, map[string]any{"kind": "analysis", "label": "R"})
	sendWant(t, srv, alice, nil, http.StatusCreated, "POST", "/v1/lineage/edges", map[string]string{"from": s, "to": r, "relation": "used"}, nil)

	var cs struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	sendWant(t, srv, bot, forAlice, http.StatusCreated, "POST", "/v1/change-sets", map[string]any{
		"title":  "relabel",
		"reason": "naming convention",
		"items": []map[string]any{
			{"op": "add_node", "kind": "sample", "label": "N-042"},
			{"op": "update_node", "node_id": s, "label": "P-042"},
		},
	}, &cs)
	if cs.State != "proposed" {
		t.Fatalf("new change set state %q, want proposed", cs.State)
	}

	// labelsOf maps the node ids in a lineage closure to their labels.
	labelsOf := func(id, direction string) map[string]string {
		var lin struct {
			Nodes []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"nodes"`
		}
		sendWant(t, srv, alice, nil, http.StatusOK, "GET", "/v1/lineage/"+id+"?direction="+direction, nil, &lin)
		got := map[string]string{}
		for _, n := range lin.Nodes {
			got[n.ID] = n.Label
		}
		return got
	}
	countLabel := func(label string) int {
		var n int
		err := db.WithTenant(ctx, f.tenant, storage.Principal{}, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM lineage_nodes WHERE label = $1`, label).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := labelsOf(s, "both")[s]; got != "Plasma 042" {
		t.Errorf("S before commit labelled %q, want Plasma 042", got)
	}
	for id, label := range labelsOf(s, "both") {
		if label == "N-042" {
			t.Errorf("node %s labelled N-042 before commit", id)
		}
	}
	for id, label := range labelsOf(r, "upstream") {
		if label == "N-042" {
			t.Errorf("upstream of R includes %s labelled N-042 before commit", id)
		}
	}
	if n := countLabel("N-042"); n != 0 {
		t.Errorf("%d lineage nodes labelled N-042 before commit, want 0", n)
	}

	var committed struct {
		State string `json:"state"`
	}
	sendWant(t, srv, alice, nil, http.StatusOK, "POST", "/v1/change-sets/"+cs.ID+"/commit", map[string]string{"reason": "ok"}, &committed)
	if committed.State != "committed" {
		t.Errorf("state after commit %q, want committed", committed.State)
	}

	if got := labelsOf(s, "both")[s]; got != "P-042" {
		t.Errorf("S after commit labelled %q, want P-042", got)
	}
	if n := countLabel("N-042"); n != 1 {
		t.Errorf("%d lineage nodes labelled N-042 after commit, want 1", n)
	}
}

// SPEC-007-R04
func TestCommitIsAtomic(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tenant := newTenant(t, db)
	srv := httptest.NewServer(api.New(config.Config{Env: "dev"}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).Handler())
	defer srv.Close()
	devHeaders := map[string]string{"X-LDE-Tenant": tenant, "X-LDE-Actor": "it@lab"}

	s := createNode(t, srv, "", devHeaders, map[string]any{"kind": "sample", "label": "Plasma 042", "attrs": map[string]any{"volume_ul": 50}})
	r := createNode(t, srv, "", devHeaders, map[string]any{"kind": "analysis", "label": "R"})

	var cs struct {
		ID string `json:"id"`
	}
	sendWant(t, srv, "", devHeaders, http.StatusCreated, "POST", "/v1/change-sets", map[string]any{
		"title":  "atomic",
		"reason": "cycle test",
		"items": []map[string]any{
			{"op": "update_node", "node_id": s, "attrs": map[string]any{"volume_ul": 45}},
			{"op": "add_edge", "from": s, "to": r, "relation": "used"},
			{"op": "add_edge", "from": r, "to": s, "relation": "used"},
		},
	}, &cs)

	n := auditCount(t, db, tenant)
	sendWant(t, srv, "", devHeaders, http.StatusConflict, "POST", "/v1/change-sets/"+cs.ID+"/commit", map[string]string{"reason": "ok"}, nil)

	if after := auditCount(t, db, tenant); after != n {
		t.Errorf("audit events grew from %d to %d on a failed commit", n, after)
	}
	var maxRev, edges int
	var volume string
	err := db.WithTenant(ctx, tenant, storage.Principal{}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT max(rev) FROM record_revisions WHERE entity_id = $1`, s).Scan(&maxRev); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT attrs->>'volume_ul' FROM lineage_nodes WHERE id = $1`, s).Scan(&volume); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM lineage_edges WHERE from_id = $1`, s).Scan(&edges)
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxRev != 1 {
		t.Errorf("S max revision %d, want 1", maxRev)
	}
	if volume != "50" {
		t.Errorf("S volume_ul %q, want 50", volume)
	}
	if edges != 0 {
		t.Errorf("%d edges from S, want 0", edges)
	}

	var cur struct {
		State string `json:"state"`
	}
	sendWant(t, srv, "", devHeaders, http.StatusOK, "GET", "/v1/change-sets/"+cs.ID, nil, &cur)
	if cur.State != "proposed" {
		t.Errorf("state after failed commit %q, want proposed", cur.State)
	}
}

// SPEC-007-R10
func TestSealedCommitConflicts(t *testing.T) {
	db := openTestDB(t)
	tenant := newTenant(t, db)
	srv := httptest.NewServer(api.New(config.Config{Env: "dev"}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).Handler())
	defer srv.Close()
	devHeaders := map[string]string{"X-LDE-Tenant": tenant, "X-LDE-Actor": "it@lab"}

	var exp struct {
		ID string `json:"id"`
	}
	sendWant(t, srv, "", devHeaders, http.StatusCreated, "POST", "/v1/experiments", map[string]string{"slug": "e", "title": "E", "protocol_md": "# A\n"}, &exp)
	newRun := func() string {
		var run struct {
			ID string `json:"id"`
		}
		sendWant(t, srv, "", devHeaders, http.StatusCreated, "POST", "/v1/runs", map[string]string{"experiment_id": exp.ID}, &run)
		return run.ID
	}
	rid := newRun()
	aid := newRun()
	transition := func(id, to, reason string) {
		sendWant(t, srv, "", devHeaders, http.StatusOK, "POST", "/v1/runs/"+id+"/transitions", map[string]string{"to": to, "reason": reason}, nil)
	}
	transition(rid, "running", "")
	transition(rid, "completed", "")
	transition(rid, "signed", "approved")
	transition(aid, "aborted", "contaminated")

	for _, id := range []string{rid, aid} {
		var cs struct {
			ID string `json:"id"`
		}
		sendWant(t, srv, "", devHeaders, http.StatusCreated, "POST", "/v1/change-sets", map[string]any{
			"title":  "params",
			"reason": "temperature correction",
			"items": []map[string]any{
				{"op": "update_run_params", "run_id": id, "params": map[string]any{"temp_c": 37}},
			},
		}, &cs)
		sendWant(t, srv, "", devHeaders, http.StatusConflict, "POST", "/v1/change-sets/"+cs.ID+"/commit", map[string]string{"reason": "ok"}, nil)
	}
}
