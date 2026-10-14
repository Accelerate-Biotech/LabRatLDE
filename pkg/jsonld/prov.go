// Package jsonld serialises provenance graphs as W3C PROV-O in JSON-LD, so
// lineage leaves LabRat as linked data any RDF toolchain can load: no SDK,
// no vendor, no export fee.
package jsonld

import (
	"encoding/json"
	"io"
	"sort"
)

// Resource is a PROV node. ID should be an absolute IRI (e.g. "urn:uuid:...").
type Resource struct {
	ID    string
	Class string // "prov:Entity", "prov:Activity", "prov:Agent", ...
	Label string
	Props map[string]any // emitted under the "lde:" vocabulary
}

// Statement asserts Subject <Predicate> Object, where Predicate is a PROV
// term such as "used" or "wasGeneratedBy".
type Statement struct {
	Subject, Predicate, Object string
}

// Context returns the JSON-LD @context. vocab is the IRI for LabRat-specific
// properties, e.g. "https://lde.example.org/ns#".
func Context(vocab string) map[string]any {
	ctx := map[string]any{
		"prov": "http://www.w3.org/ns/prov#",
		"rdfs": "http://www.w3.org/2000/01/rdf-schema#",
		"lde":  vocab,
	}
	for _, p := range []string{"used", "wasGeneratedBy", "wasDerivedFrom", "wasInformedBy", "wasAssociatedWith", "wasAttributedTo"} {
		ctx[p] = map[string]string{"@id": "prov:" + p, "@type": "@id"}
	}
	return ctx
}

// Encode writes a JSON-LD document with one @graph entry per resource.
// Output is deterministic for identical input.
func Encode(w io.Writer, vocab string, resources []Resource, statements []Statement) error {
	links := map[string]map[string][]string{}
	for _, s := range statements {
		if links[s.Subject] == nil {
			links[s.Subject] = map[string][]string{}
		}
		links[s.Subject][s.Predicate] = append(links[s.Subject][s.Predicate], s.Object)
	}

	graph := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		node := map[string]any{"@id": r.ID, "@type": r.Class}
		if r.Label != "" {
			node["rdfs:label"] = r.Label
		}
		for k, v := range r.Props {
			node["lde:"+k] = v
		}
		for pred, objs := range links[r.ID] {
			sort.Strings(objs)
			node[pred] = objs
		}
		graph = append(graph, node)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"@context": Context(vocab), "@graph": graph})
}
