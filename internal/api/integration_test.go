package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/api"
	"github.com/Accelerate-Biotech/LabRatLDE/internal/config"
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
	srv := httptest.NewServer(api.New(config.Config{Env: "dev"}, db, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
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
