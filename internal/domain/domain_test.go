package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// qPCR workflow: sample + primer lot feed a run, which generates raw Cq data,
// which an analysis turns into a derived result.
func qpcrGraph(t *testing.T) *Graph {
	t.Helper()
	g := NewGraph()
	for _, n := range []Node{
		{ID: "sample", Kind: KindSample}, {ID: "primers", Kind: KindReagent},
		{ID: "run", Kind: KindRun}, {ID: "raw", Kind: KindDataset},
		{ID: "ddct", Kind: KindAnalysis}, {ID: "result", Kind: KindDataset},
	} {
		if err := g.AddNode(n); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []Edge{
		{"sample", "run", RelUsed}, {"primers", "run", RelUsed},
		{"run", "raw", RelWasGeneratedBy}, {"raw", "ddct", RelUsed},
		{"ddct", "result", RelWasGeneratedBy},
	} {
		if err := g.AddEdge(e); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// SPEC-001-R09
func TestLineageRejectsCycles(t *testing.T) {
	g := qpcrGraph(t)
	for _, e := range []Edge{{"result", "sample", RelWasDerivedFrom}, {"run", "run", RelWasInformedBy}} {
		if err := g.AddEdge(e); !errors.Is(err, ErrCycle) {
			t.Errorf("AddEdge(%v) = %v, want ErrCycle", e, err)
		}
	}
}

// SPEC-001-R10
func TestLineageClosure(t *testing.T) {
	g := qpcrGraph(t)
	if got, want := g.Upstream("result"), []string{"ddct", "primers", "raw", "run", "sample"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Upstream = %v, want %v", got, want)
	}
	if got, want := g.Downstream("primers"), []string{"ddct", "raw", "result", "run"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Downstream (recall impact) = %v, want %v", got, want)
	}
}

// SPEC-001-R11, SPEC-001-R12
func TestRunLifecycle(t *testing.T) {
	now := time.Now()
	r := Run{State: RunPlanned}
	for _, s := range []struct {
		to     RunState
		reason string
	}{{RunRunning, ""}, {RunPaused, ""}, {RunRunning, ""}, {RunCompleted, ""}, {RunSigned, "approved"}} {
		if err := r.Apply(s.to, s.reason, now); err != nil {
			t.Fatalf("-> %s: %v", s.to, err)
		}
	}
	rejected := []struct {
		name string
		run  Run
		to   RunState
		why  string
	}{
		{"signed is terminal", r, RunRunning, ""},
		{"signing needs a meaning", Run{State: RunCompleted}, RunSigned, "  "},
		{"aborting needs a reason", Run{State: RunRunning}, RunAborted, ""},
		{"cannot skip running", Run{State: RunPlanned}, RunCompleted, ""},
	}
	for _, c := range rejected {
		if err := c.run.Apply(c.to, c.why, now); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s: got %v, want ErrInvalidTransition", c.name, err)
		}
	}
}
