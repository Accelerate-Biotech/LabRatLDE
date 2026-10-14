// Package frontmatter splits Markdown documents with YAML frontmatter, the
// format LabRat uses for protocols. Protocols stay plain text: diffable,
// reviewable in a pull request, and readable in fifty years without us.
//
//	---
//	protocol: qpcr-ddct
//	version: 3
//	reagents: [SYBR-Green-lot-2291]
//	---
//	# Relative quantification by ΔΔCt
//	1. Thaw master mix on ice ...
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Document is a parsed Markdown file.
type Document struct {
	Meta map[string]any
	Body []byte
}

var ErrUnterminated = errors.New("frontmatter: opening '---' has no closing '---'")

// Parse extracts YAML frontmatter delimited by '---' lines. A document with
// no frontmatter is valid and yields empty Meta.
func Parse(src []byte) (Document, error) {
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf")) // UTF-8 BOM
	norm := bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))

	doc := Document{Meta: map[string]any{}}
	if !bytes.HasPrefix(norm, []byte("---\n")) {
		doc.Body = src
		return doc, nil
	}
	rest := norm[len("---\n"):]

	var yamlBlock, body []byte
	switch {
	case bytes.HasPrefix(rest, []byte("---\n")) || bytes.Equal(rest, []byte("---")):
		body = bytes.TrimPrefix(bytes.TrimPrefix(rest, []byte("---")), []byte("\n"))
	default:
		end := bytes.Index(rest, []byte("\n---\n"))
		switch {
		case end >= 0:
			yamlBlock, body = rest[:end], rest[end+len("\n---\n"):]
		case bytes.HasSuffix(rest, []byte("\n---")):
			yamlBlock = rest[:len(rest)-len("\n---")]
		default:
			return Document{}, ErrUnterminated
		}
	}

	if err := yaml.Unmarshal(yamlBlock, &doc.Meta); err != nil {
		return Document{}, fmt.Errorf("frontmatter: invalid YAML: %w", err)
	}
	if doc.Meta == nil {
		doc.Meta = map[string]any{}
	}
	doc.Body = body
	return doc, nil
}
