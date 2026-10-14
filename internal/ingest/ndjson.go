// Package ingest decodes high-throughput instrument streams.
//
// The wire format is NDJSON, one reading per line, so any instrument PC that
// can run curl, Python, or a LabVIEW HTTP VI can stream into LabRat without a
// driver or SDK:
//
//	{"ts":"2026-10-14T09:30:00.125Z","channel":"UV_254nm","value":0.4312,"unit":"AU"}
package ingest

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// MaxLineBytes bounds a single reading so one bad client can't exhaust memory.
const MaxLineBytes = 64 << 10

type Reading struct {
	TS      time.Time `json:"ts"`
	Channel string    `json:"channel"`
	Value   float64   `json:"value"`
	Unit    string    `json:"unit,omitempty"`
}

// Summary describes a fully consumed stream. SHA256 covers the exact bytes
// received, so the audit event pins the raw upload, not our parse of it.
type Summary struct {
	Count  int64     `json:"count"`
	SHA256 string    `json:"sha256"`
	First  time.Time `json:"first_ts"`
	Last   time.Time `json:"last_ts"`
}

// LineError locates a malformed reading.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }
func (e *LineError) Unwrap() error { return e.Err }

// Decode reads NDJSON from r and hands readings to flush in batches of at most
// batchSize. Ingestion is all-or-nothing at the transaction layer, so on any
// error the caller should roll back.
func Decode(r io.Reader, batchSize int, flush func([]Reading) error) (Summary, error) {
	if batchSize <= 0 {
		batchSize = 5000
	}
	h := sha256.New()
	sc := bufio.NewScanner(io.TeeReader(r, h))
	sc.Buffer(make([]byte, 0, 64<<10), MaxLineBytes)

	var (
		sum   Summary
		batch = make([]Reading, 0, batchSize)
		line  int
	)
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(b) == 0 || (len(b) == 1 && b[0] == '\r') {
			continue
		}
		var rd Reading
		if err := json.Unmarshal(b, &rd); err != nil {
			return sum, &LineError{line, err}
		}
		if err := rd.validate(); err != nil {
			return sum, &LineError{line, err}
		}
		if sum.Count == 0 || rd.TS.Before(sum.First) {
			sum.First = rd.TS
		}
		if rd.TS.After(sum.Last) {
			sum.Last = rd.TS
		}
		sum.Count++
		batch = append(batch, rd)
		if len(batch) == batchSize {
			if err := flush(batch); err != nil {
				return sum, err
			}
			batch = batch[:0]
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return sum, &LineError{line + 1, fmt.Errorf("reading exceeds %d bytes", MaxLineBytes)}
		}
		return sum, err
	}
	if len(batch) > 0 {
		if err := flush(batch); err != nil {
			return sum, err
		}
	}
	sum.SHA256 = hex.EncodeToString(h.Sum(nil))
	return sum, nil
}

func (r Reading) validate() error {
	switch {
	case r.TS.IsZero():
		return errors.New("ts is required (RFC 3339)")
	case r.Channel == "":
		return errors.New("channel is required")
	case math.IsNaN(r.Value) || math.IsInf(r.Value, 0):
		return errors.New("value must be finite")
	}
	return nil
}
