package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"github.com/Accelerate-Biotech/LabRatLDE/internal/domain"
)

func TestParquetRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 14, 9, 30, 0, 125_000_000, time.UTC)
	in := []ReadingRow{
		RowFrom(ts, "hplc-1", "ds-1", "UV_254nm", 0.4312, "AU"),
		RowFrom(ts.Add(time.Second), "hplc-1", "ds-1", "pressure", 212.5, "bar"),
	}
	var buf bytes.Buffer
	w := NewParquetWriter(&buf)
	if err := w.Write(in); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := parquet.Read[ReadingRow](bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestProvJSONLD(t *testing.T) {
	g := domain.NewGraph()
	_ = g.AddNode(domain.Node{ID: "s", Kind: domain.KindSample, Label: "Plasma 042"})
	_ = g.AddNode(domain.Node{ID: "r", Kind: domain.KindRun, Label: "qPCR plate 7"})
	if err := g.AddEdge(domain.Edge{From: "s", To: "r", Relation: domain.RelUsed}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := ProvJSONLD(&buf, "https://lde.example.org/ns#", g); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var run map[string]any
	for _, n := range doc.Graph {
		if n["@id"] == "urn:uuid:r" {
			run = n
		}
	}
	if run == nil || run["@type"] != "prov:Activity" {
		t.Fatalf("run not emitted as prov:Activity: %s", buf.String())
	}
	if used, _ := run["used"].([]any); len(used) != 1 || used[0] != "urn:uuid:s" {
		t.Fatalf("expected run prov:used sample, got %v", run["used"])
	}
	if !strings.Contains(buf.String(), `"prov": "http://www.w3.org/ns/prov#"`) {
		t.Error("missing PROV context")
	}
}
