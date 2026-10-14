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

func TestLineageRejectsCycles(t *testing.T) {
	g := qpcrGraph(t)
	for _, e := range []Edge{{"result", "sample", RelWasDerivedFrom}, {"run", "run", RelWasInformedBy}} {
		if err := g.AddEdge(e); !errors.Is(err, ErrCycle) {
			t.Errorf("AddEdge(%v) = %v, want ErrCycle", e, err)
		}
	}
}

func TestLineageRejectsUnknownAndInvalid(t *testing.T) {
	g := qpcrGraph(t)
	if err := g.AddEdge(Edge{"sample", "ghost", RelUsed}); !errors.Is(err, ErrUnknownNode) {
		t.Errorf("got %v, want ErrUnknownNode", err)
	}
	if err := g.AddEdge(Edge{"sample", "raw", "inspired"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("got %v, want ErrInvalid", err)
	}
	if err := g.AddNode(Node{ID: "sample", Kind: KindSample}); !errors.Is(err, ErrDuplicateNode) {
		t.Errorf("got %v, want ErrDuplicateNode", err)
	}
}

func TestLineageTraversal(t *testing.T) {
	g := qpcrGraph(t)
	if got, want := g.Upstream("result"), []string{"ddct", "primers", "raw", "run", "sample"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Upstream = %v, want %v", got, want)
	}
	if got, want := g.Downstream("primers"), []string{"ddct", "raw", "result", "run"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Downstream (recall blast radius) = %v, want %v", got, want)
	}
	var order []string
	for _, n := range g.Nodes() {
		order = append(order, n.ID)
	}
	if want := []string{"primers", "sample", "run", "raw", "ddct", "result"}; !reflect.DeepEqual(order, want) {
		t.Errorf("topological order = %v, want %v", order, want)
	}
}

func TestRunLifecycle(t *testing.T) {
	now := time.Now()
	r := Run{State: RunPlanned}
	steps := []struct {
		to     RunState
		reason string
	}{{RunRunning, ""}, {RunPaused, ""}, {RunRunning, ""}, {RunCompleted, ""}, {RunSigned, "approved"}}
	for _, s := range steps {
		if err := r.Apply(s.to, s.reason, now); err != nil {
			t.Fatalf("-> %s: %v", s.to, err)
		}
	}
	if r.StartedAt == nil || r.FinishedAt == nil || !r.State.Terminal() {
		t.Fatalf("unexpected final run: %+v", r)
	}
	if err := r.Apply(RunRunning, "", now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("signed run must be immutable, got %v", err)
	}
}

func TestRunSigningRequiresMeaning(t *testing.T) {
	r := Run{State: RunCompleted}
	if err := r.Apply(RunSigned, "  ", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
	if err := (&Run{State: RunPlanned}).Apply(RunCompleted, "", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("planned -> completed must be rejected, got %v", err)
	}
}
