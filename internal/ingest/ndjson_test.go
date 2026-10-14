package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestDecodeBatchesAndHashes(t *testing.T) {
	raw := `{"ts":"2026-10-14T09:30:00.2Z","channel":"UV_254nm","value":0.43,"unit":"AU"}
{"ts":"2026-10-14T09:30:00.1Z","channel":"UV_254nm","value":0.41,"unit":"AU"}

{"ts":"2026-10-14T09:30:00.3Z","channel":"pressure","value":212.5,"unit":"bar"}
`
	var batches []int
	sum, err := Decode(strings.NewReader(raw), 2, func(b []Reading) error {
		batches = append(batches, len(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Count != 3 || len(batches) != 2 || batches[0] != 2 || batches[1] != 1 {
		t.Fatalf("count=%d batches=%v", sum.Count, batches)
	}
	if got := sum.First.Format("05.0"); got != "00.1" {
		t.Errorf("first ts = %s", got)
	}
	want := sha256.Sum256([]byte(raw))
	if sum.SHA256 != hex.EncodeToString(want[:]) {
		t.Error("summary hash must cover the raw bytes received")
	}
}

func TestDecodeRejectsBadLines(t *testing.T) {
	for name, raw := range map[string]string{
		"no ts":      `{"channel":"a","value":1}`,
		"no channel": `{"ts":"2026-10-14T09:30:00Z","value":1}`,
		"not json":   `ts=now value=1`,
		"too long":   `{"channel":"` + strings.Repeat("x", MaxLineBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader("\n"+raw+"\n"), 10, func([]Reading) error { return nil })
			var le *LineError
			if !errors.As(err, &le) || le.Line != 2 {
				t.Fatalf("want LineError at line 2, got %v", err)
			}
		})
	}
}
