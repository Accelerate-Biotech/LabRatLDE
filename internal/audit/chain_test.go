package audit

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const tenant = "00000000-0000-4000-8000-000000000001"

func buildChain(t *testing.T, n int) []Event {
	t.Helper()
	head := Head{Hash: Genesis}
	at := time.Date(2026, 10, 14, 9, 30, 0, 123456789, time.UTC)
	events := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		ev, err := Seal(tenant, head, at.Add(time.Duration(i)*time.Second), Entry{
			Actor: "analyst@lab", Action: "run.transitioned", EntityType: "run",
			EntityID: "r-1", Payload: map[string]any{"to": "running", "step": i},
		})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ev)
		head = Head{Seq: ev.Seq, Hash: ev.Hash}
	}
	return events
}

func verifyAll(events []Event) Report {
	var v Verifier
	for _, ev := range events {
		if err := v.Next(ev); err != nil {
			return v.Report(err)
		}
	}
	return v.Report(nil)
}

func TestSealLinksEvents(t *testing.T) {
	events := buildChain(t, 3)
	if events[0].PrevHash != Genesis || events[0].Seq != 1 {
		t.Fatalf("first event must chain from genesis at seq 1, got seq %d", events[0].Seq)
	}
	for i := 1; i < len(events); i++ {
		if events[i].PrevHash != events[i-1].Hash {
			t.Fatalf("event %d not linked to predecessor", i)
		}
	}
	if events[0].OccurredAt.Nanosecond()%1000 != 0 {
		t.Fatal("timestamps must be truncated to microseconds to match Postgres")
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	cases := map[string]func([]Event){
		"altered payload": func(e []Event) { e[2].Payload = json.RawMessage(`{"step":2,"to":"completed"}`) },
		"altered actor":   func(e []Event) { e[2].Actor = "mallory@lab" },
		"backdated":       func(e []Event) { e[2].OccurredAt = e[2].OccurredAt.Add(-time.Hour) },
		"deleted event":   func(e []Event) { copy(e[2:], e[3:]) },
		"swapped order":   func(e []Event) { e[1], e[2] = e[2], e[1] },
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			events := buildChain(t, 5)
			if r := verifyAll(events); !r.Valid || r.Events != 5 {
				t.Fatalf("pristine chain should verify: %+v", r)
			}
			tamper(events)
			r := verifyAll(events)
			if r.Valid || r.Break == nil {
				t.Fatal("tampering went undetected")
			}
			if r.Break.Seq < 2 || r.Break.Seq > 4 {
				t.Fatalf("break reported at seq %d, want 2..4", r.Break.Seq)
			}
		})
	}
}

func TestCanonicalizeIsDeterministic(t *testing.T) {
	a, err := Canonicalize(json.RawMessage(`{ "b": 1, "a": {"y": 12345678901234567890, "x": "<ok>"} }`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonicalize(map[string]any{"a": map[string]any{"x": "<ok>", "y": json.Number("12345678901234567890")}, "b": 1})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"a":{"x":"<ok>","y":12345678901234567890},"b":1}`
	if string(a) != want || string(b) != want {
		t.Fatalf("got %s and %s, want %s", a, b, want)
	}
}

func TestCanonicalizeRejectsGarbage(t *testing.T) {
	for _, in := range []string{`{`, `{} {}`, `nope`} {
		if _, err := Canonicalize(json.RawMessage(in)); err == nil {
			t.Errorf("Canonicalize(%q) should fail", in)
		}
	}
}

// TestDigestVector pins the v1 encoding. If this fails you changed the hash
// format: every existing chain would stop verifying. Bump DigestVersion instead.
func TestDigestVector(t *testing.T) {
	ev := Event{
		TenantID: tenant, Seq: 1, OccurredAt: time.Unix(1_760_000_000, 0).UTC(),
		Actor: "a", Action: "b", EntityType: "c", EntityID: "d", Payload: json.RawMessage(`{}`),
	}
	got := Digest(&ev).String()
	const want = "1c77a6a958a31b8539c0244258721ab446721171aadae8b9fca1c6830817f1f5"
	if got != want {
		t.Fatalf("digest changed: got %s want %s", got, want)
	}
}

func TestBreakErrorUnwrap(t *testing.T) {
	var v Verifier
	err := v.Next(Event{Seq: 7})
	var be *BreakError
	if !errors.As(err, &be) || be.Seq != 7 {
		t.Fatalf("want BreakError at seq 7, got %v", err)
	}
}
