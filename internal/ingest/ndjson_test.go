package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// SPEC-001-R13
func TestSummaryHashesRawBytes(t *testing.T) {
	raw := `{"ts":"2026-10-14T09:30:00.2Z","channel":"UV_254nm","value":0.43,"unit":"AU"}
{"ts":"2026-10-14T09:30:00.1Z","channel":"UV_254nm","value":0.41,"unit":"AU"}

{"ts":"2026-10-14T09:30:00.3Z","channel":"pressure","value":212.5,"unit":"bar"}
`
	sum, err := Decode(strings.NewReader(raw), 2, func([]Reading) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(raw))
	if sum.Count != 3 || sum.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("count=%d sha256=%s, want 3 and the hash of the raw body", sum.Count, sum.SHA256)
	}
}
