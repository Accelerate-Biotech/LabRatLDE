// Package audit implements the append-only, hash-chained event ledger that
// backs LabRat's 21 CFR Part 11 audit trail.
//
// Every state change in the system is recorded as an Event. Each Event commits
// to its predecessor via PrevHash, so altering, deleting, or reordering any
// historical record breaks every hash after it. The digest format is specified
// in docs/audit-chain.md and is implemented identically in Go (Digest) and in
// PostgreSQL (audit_digest), so the database rejects forged events on insert
// and any third party can re-verify an exported chain with ~30 lines of code.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DigestVersion is the domain-separation tag mixed into every digest. Bump it
// (and keep the old verifier) if the encoding ever changes.
const DigestVersion = "LDE-AUDIT-v1"

// Hash is a SHA-256 digest.
type Hash [sha256.Size]byte

// Genesis is the PrevHash of the first event in every tenant chain.
var Genesis Hash

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

func (h Hash) MarshalText() ([]byte, error) { return []byte(h.String()), nil }

func (h *Hash) UnmarshalText(b []byte) error {
	n, err := hex.Decode(h[:], b)
	if err != nil {
		return fmt.Errorf("audit: decode hash: %w", err)
	}
	if n != sha256.Size {
		return fmt.Errorf("audit: hash must be %d bytes, got %d", sha256.Size, n)
	}
	return nil
}

// HashFromBytes copies a 32-byte slice (e.g. a bytea column) into a Hash.
func HashFromBytes(b []byte) (Hash, error) {
	var h Hash
	if len(b) != sha256.Size {
		return h, fmt.Errorf("audit: hash must be %d bytes, got %d", sha256.Size, len(b))
	}
	copy(h[:], b)
	return h, nil
}

// Entry is what callers supply; the ledger turns it into a sealed Event.
type Entry struct {
	Actor      string // authenticated user or service principal
	Action     string // dotted verb, e.g. "run.transitioned"
	EntityType string
	EntityID   string
	// Reason is the human-supplied meaning of the action. For electronic
	// signatures it carries the signature meaning required by §11.50
	// ("approved", "reviewed", "authored", ...).
	Reason  string
	Payload any
}

// Head is the tip of a tenant's chain.
type Head struct {
	Seq  int64 `json:"seq"`
	Hash Hash  `json:"hash"`
}

// Event is a sealed, immutable audit record.
type Event struct {
	TenantID   string          `json:"tenant_id"`
	Seq        int64           `json:"seq"`
	OccurredAt time.Time       `json:"occurred_at"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Reason     string          `json:"reason,omitempty"`
	Payload    json.RawMessage `json:"payload"` // canonical JSON; exactly the bytes that were hashed
	PrevHash   Hash            `json:"prev_hash"`
	Hash       Hash            `json:"hash"`
}

// Record is the export form of an Event. Payload carries the exact canonical
// bytes that were hashed as a JSON string, so a verifier in any language can
// recompute the digest without reimplementing JSON canonicalisation.
type Record struct {
	TenantID   string    `json:"tenant_id"`
	Seq        int64     `json:"seq"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Reason     string    `json:"reason"`
	Payload    string    `json:"payload"`
	PrevHash   Hash      `json:"prev_hash"`
	Hash       Hash      `json:"hash"`
}

func (ev Event) Record() Record {
	return Record{
		TenantID: ev.TenantID, Seq: ev.Seq, OccurredAt: ev.OccurredAt, Actor: ev.Actor, Action: ev.Action,
		EntityType: ev.EntityType, EntityID: ev.EntityID, Reason: ev.Reason, Payload: string(ev.Payload),
		PrevHash: ev.PrevHash, Hash: ev.Hash,
	}
}

// Seal links e onto head and computes its hash. at is truncated to
// microseconds because that is PostgreSQL's timestamptz resolution.
func Seal(tenantID string, head Head, at time.Time, e Entry) (Event, error) {
	if e.Actor == "" || e.Action == "" || e.EntityType == "" || e.EntityID == "" {
		return Event{}, errors.New("audit: actor, action, entity_type and entity_id are required")
	}
	payload, err := Canonicalize(e.Payload)
	if err != nil {
		return Event{}, err
	}
	ev := Event{
		TenantID:   strings.ToLower(tenantID),
		Seq:        head.Seq + 1,
		OccurredAt: at.UTC().Truncate(time.Microsecond),
		Actor:      e.Actor,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Reason:     e.Reason,
		Payload:    payload,
		PrevHash:   head.Hash,
	}
	ev.Hash = Digest(&ev)
	return ev, nil
}

// Digest computes the v1 event hash:
//
//	SHA-256( "LDE-AUDIT-v1" || u64(seq) || i64(unix_micros) ||
//	         lp(tenant_id) || lp(actor) || lp(action) || lp(entity_type) ||
//	         lp(entity_id) || lp(reason) || lp(payload) || prev_hash )
//
// where integers are big-endian and lp(x) = u64(len(x)) || x. Length prefixes
// make the encoding unambiguous without any escaping rules.
func Digest(ev *Event) Hash {
	h := sha256.New()
	h.Write([]byte(DigestVersion))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(ev.Seq))
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], uint64(ev.OccurredAt.UnixMicro()))
	h.Write(buf[:])
	for _, f := range []string{ev.TenantID, ev.Actor, ev.Action, ev.EntityType, ev.EntityID, ev.Reason, string(ev.Payload)} {
		binary.BigEndian.PutUint64(buf[:], uint64(len(f)))
		h.Write(buf[:])
		h.Write([]byte(f))
	}
	h.Write(ev.PrevHash[:])
	var out Hash
	h.Sum(out[:0])
	return out
}

// Canonicalize renders v as deterministic JSON: object keys sorted, no
// insignificant whitespace, numbers preserved verbatim, no HTML escaping.
// A nil payload becomes "{}".
func Canonicalize(v any) (json.RawMessage, error) {
	var raw []byte
	switch x := v.(type) {
	case nil:
		return json.RawMessage("{}"), nil
	case json.RawMessage:
		raw = x
	case []byte:
		raw = x
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("audit: marshal payload: %w", err)
		}
		raw = b
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("audit: payload is not valid JSON: %w", err)
	}
	if dec.More() {
		return nil, errors.New("audit: payload contains trailing data")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil { // encoding/json sorts map keys
		return nil, fmt.Errorf("audit: canonicalize payload: %w", err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// BreakError pinpoints the first event at which a chain fails verification.
type BreakError struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

func (e *BreakError) Error() string {
	return fmt.Sprintf("audit chain broken at seq %d: %s", e.Seq, e.Reason)
}

// Verifier re-derives a chain one event at a time. It holds O(1) state, so it
// can verify arbitrarily long chains streamed from the database or a file.
type Verifier struct {
	head   Head
	events int64
}

// Next checks ev against the running head and advances it.
func (v *Verifier) Next(ev Event) error {
	switch {
	case ev.Seq != v.head.Seq+1:
		return &BreakError{Seq: ev.Seq, Reason: fmt.Sprintf("expected seq %d", v.head.Seq+1)}
	case ev.PrevHash != v.head.Hash:
		return &BreakError{Seq: ev.Seq, Reason: "prev_hash does not match preceding event"}
	case Digest(&ev) != ev.Hash:
		return &BreakError{Seq: ev.Seq, Reason: "content does not match stored hash (record altered)"}
	}
	v.head = Head{Seq: ev.Seq, Hash: ev.Hash}
	v.events++
	return nil
}

// Report summarises a verification pass.
type Report struct {
	Valid  bool        `json:"valid"`
	Events int64       `json:"events"`
	Head   Head        `json:"head"`
	Break  *BreakError `json:"break,omitempty"`
}

// Report returns the outcome so far; brk is the error returned by Next, if any.
func (v *Verifier) Report(brk error) Report {
	r := Report{Valid: brk == nil, Events: v.events, Head: v.head}
	var be *BreakError
	if errors.As(brk, &be) {
		r.Break = be
	}
	return r
}
