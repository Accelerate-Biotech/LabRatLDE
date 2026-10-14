// Package domain holds LabRat's core scientific models: the provenance graph
// that connects every sample, run, and dataset, and the lifecycle of an
// experiment run. It has no I/O dependencies.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// NodeKind classifies a vertex in the lineage graph.
type NodeKind string

const (
	KindSample     NodeKind = "sample"
	KindReagent    NodeKind = "reagent"
	KindProtocol   NodeKind = "protocol"
	KindRun        NodeKind = "run"
	KindInstrument NodeKind = "instrument"
	KindDataset    NodeKind = "dataset"
	KindAnalysis   NodeKind = "analysis"
)

func (k NodeKind) Valid() bool {
	switch k {
	case KindSample, KindReagent, KindProtocol, KindRun, KindInstrument, KindDataset, KindAnalysis:
		return true
	}
	return false
}

// Relation is the W3C PROV relation an edge asserts.
//
// Edges always point downstream, in the direction data flows: From is the
// antecedent, To is the consequent, and the triple reads "To <relation> From".
//
//	sample  --used-------------> run      (run used sample)
//	run     --wasGeneratedBy---> dataset  (dataset wasGeneratedBy run)
//	dataset --wasDerivedFrom---> dataset  (derived wasDerivedFrom raw)
//	instrument --wasAttributedTo-> dataset (dataset wasAttributedTo instrument)
type Relation string

const (
	RelUsed              Relation = "used"
	RelWasGeneratedBy    Relation = "wasGeneratedBy"
	RelWasDerivedFrom    Relation = "wasDerivedFrom"
	RelWasInformedBy     Relation = "wasInformedBy"
	RelWasAssociatedWith Relation = "wasAssociatedWith"
	RelWasAttributedTo   Relation = "wasAttributedTo"
)

func (r Relation) Valid() bool {
	switch r {
	case RelUsed, RelWasGeneratedBy, RelWasDerivedFrom, RelWasInformedBy, RelWasAssociatedWith, RelWasAttributedTo:
		return true
	}
	return false
}

// Node is a vertex: a physical sample, a process, or a piece of data.
type Node struct {
	ID        string         `json:"id"`
	Kind      NodeKind       `json:"kind"`
	Label     string         `json:"label"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Edge is a directed provenance link.
type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Relation Relation `json:"relation"`
}

var (
	ErrCycle         = errors.New("lineage: edge would create a cycle")
	ErrUnknownNode   = errors.New("lineage: unknown node")
	ErrDuplicateNode = errors.New("lineage: duplicate node")
	ErrInvalid       = errors.New("lineage: invalid node or edge")
)

// Graph is an in-memory lineage DAG. The database enforces acyclicity on
// write; Graph re-enforces it so traversal code can rely on it unconditionally.
type Graph struct {
	nodes map[string]Node
	out   map[string][]Edge
	in    map[string][]Edge
}

func NewGraph() *Graph {
	return &Graph{nodes: map[string]Node{}, out: map[string][]Edge{}, in: map[string][]Edge{}}
}

func (g *Graph) AddNode(n Node) error {
	if n.ID == "" || !n.Kind.Valid() {
		return fmt.Errorf("%w: node %q kind %q", ErrInvalid, n.ID, n.Kind)
	}
	if _, ok := g.nodes[n.ID]; ok {
		return fmt.Errorf("%w: %s", ErrDuplicateNode, n.ID)
	}
	g.nodes[n.ID] = n
	return nil
}

func (g *Graph) AddEdge(e Edge) error {
	if !e.Relation.Valid() {
		return fmt.Errorf("%w: relation %q", ErrInvalid, e.Relation)
	}
	for _, id := range []string{e.From, e.To} {
		if _, ok := g.nodes[id]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownNode, id)
		}
	}
	if e.From == e.To || g.reaches(e.To, e.From) {
		return fmt.Errorf("%w: %s -> %s", ErrCycle, e.From, e.To)
	}
	g.out[e.From] = append(g.out[e.From], e)
	g.in[e.To] = append(g.in[e.To], e)
	return nil
}

func (g *Graph) Node(id string) (Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// Nodes returns all nodes in topological order (antecedents first), with ties
// broken by ID so output is stable across runs.
func (g *Graph) Nodes() []Node {
	indeg := make(map[string]int, len(g.nodes))
	for id := range g.nodes {
		indeg[id] = len(g.in[id])
	}
	var ready []string
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	out := make([]Node, 0, len(g.nodes))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, g.nodes[id])
		var next []string
		for _, e := range g.out[id] {
			if indeg[e.To]--; indeg[e.To] == 0 {
				next = append(next, e.To)
			}
		}
		sort.Strings(next)
		ready = mergeSorted(ready, next)
	}
	return out
}

// Edges returns every edge, ordered by (From, To, Relation).
func (g *Graph) Edges() []Edge {
	var out []Edge
	for _, es := range g.out {
		out = append(out, es...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Relation < b.Relation
	})
	return out
}

// Upstream returns every node id that id was (transitively) derived from.
func (g *Graph) Upstream(id string) []string {
	return g.walk(id, g.in, func(e Edge) string { return e.From })
}

// Downstream returns every node id (transitively) derived from id. This is
// the blast radius when a reagent lot is recalled.
func (g *Graph) Downstream(id string) []string {
	return g.walk(id, g.out, func(e Edge) string { return e.To })
}

func (g *Graph) walk(start string, adj map[string][]Edge, next func(Edge) string) []string {
	seen := map[string]bool{start: true}
	stack := []string{start}
	var out []string
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range adj[id] {
			if n := next(e); !seen[n] {
				seen[n] = true
				out = append(out, n)
				stack = append(stack, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (g *Graph) reaches(from, to string) bool {
	for _, id := range g.Downstream(from) {
		if id == to {
			return true
		}
	}
	return false
}

func mergeSorted(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		if a[0] <= b[0] {
			out, a = append(out, a[0]), a[1:]
		} else {
			out, b = append(out, b[0]), b[1:]
		}
	}
	return append(append(out, a...), b...)
}
