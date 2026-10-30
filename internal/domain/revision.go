package domain

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Revision is one saved value of a revisable record. Rev counts from 1.
// Value is the record's full state at that revision.
type Revision struct {
	Rev         int            `json:"rev"`
	Value       map[string]any `json:"value"`
	Author      string         `json:"author"`
	Reason      string         `json:"reason"`
	ChangeSetID *string        `json:"change_set_id"`
	CreatedAt   time.Time      `json:"created_at"`
}

// Change is one difference between two revisions. Kind is "added", "removed"
// or "changed". From is set for removed and changed; To for added and changed.
type Change struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	From any    `json:"from,omitempty"`
	To   any    `json:"to,omitempty"`
}

// BlameEntry attributes one leaf of a record's latest value to the revision
// that last changed it.
type BlameEntry struct {
	Path   string    `json:"path"`
	Rev    int       `json:"rev"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
}

// identKey matches object keys that can be written as .key in a JSON path.
var identKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Diff returns the field-level differences from one value to another. Paths
// start at "$". Object keys are visited in sorted order and array indexes in
// ascending order. A key or index present on one side only is added or
// removed, with its whole subtree as the value.
func Diff(from, to map[string]any) []Change {
	return diffValue("$", from, to, nil)
}

func diffValue(path string, from, to any, out []Change) []Change {
	switch a := from.(type) {
	case map[string]any:
		if b, ok := to.(map[string]any); ok {
			return diffObject(path, a, b, out)
		}
	case []any:
		if b, ok := to.([]any); ok {
			return diffArray(path, a, b, out)
		}
	}
	if !reflect.DeepEqual(from, to) {
		out = append(out, Change{Path: path, Kind: "changed", From: from, To: to})
	}
	return out
}

func diffObject(path string, from, to map[string]any, out []Change) []Change {
	keys := make([]string, 0, len(from)+len(to))
	for k := range from {
		keys = append(keys, k)
	}
	for k := range to {
		if _, ok := from[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := jsonPath(path, k)
		a, inFrom := from[k]
		b, inTo := to[k]
		switch {
		case !inTo:
			out = append(out, Change{Path: p, Kind: "removed", From: a})
		case !inFrom:
			out = append(out, Change{Path: p, Kind: "added", To: b})
		default:
			out = diffValue(p, a, b, out)
		}
	}
	return out
}

func diffArray(path string, from, to []any, out []Change) []Change {
	for i := 0; i < len(from) || i < len(to); i++ {
		p := path + "[" + strconv.Itoa(i) + "]"
		switch {
		case i >= len(to):
			out = append(out, Change{Path: p, Kind: "removed", From: from[i]})
		case i >= len(from):
			out = append(out, Change{Path: p, Kind: "added", To: to[i]})
		default:
			out = diffValue(p, from[i], to[i], out)
		}
	}
	return out
}

// jsonPath appends an object key to a path. Keys that are plain identifiers
// become .key; any other key becomes ["key"], JSON-escaped.
func jsonPath(parent, key string) string {
	if identKey.MatchString(key) {
		return parent + "." + key
	}
	b, _ := json.Marshal(key)
	return parent + "[" + string(b) + "]"
}

// Blame attributes each leaf of the newest revision to the earliest revision
// from which it has held its current value without a break. revs must be
// ordered oldest first. A leaf is a scalar, null, {} or []. With no revisions
// it returns an empty slice.
func Blame(revs []Revision) []BlameEntry {
	values := map[string]any{}
	entries := map[string]BlameEntry{}
	for _, r := range revs {
		leaves := map[string]any{}
		flatten("$", r.Value, leaves)
		for p, v := range leaves {
			if old, ok := values[p]; !ok || !reflect.DeepEqual(old, v) {
				values[p] = v
				entries[p] = BlameEntry{Path: p, Rev: r.Rev, Author: r.Author, At: r.CreatedAt}
			}
		}
		for p := range values {
			if _, ok := leaves[p]; !ok {
				delete(values, p)
				delete(entries, p)
			}
		}
	}
	paths := make([]string, 0, len(entries))
	for p := range entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]BlameEntry, 0, len(paths))
	for _, p := range paths {
		out = append(out, entries[p])
	}
	return out
}

// flatten writes every leaf under value to out, keyed by path.
func flatten(path string, value any, out map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) > 0 {
			for k, c := range v {
				flatten(jsonPath(path, k), c, out)
			}
			return
		}
	case []any:
		if len(v) > 0 {
			for i, c := range v {
				flatten(path+"["+strconv.Itoa(i)+"]", c, out)
			}
			return
		}
	}
	out[path] = value
}
