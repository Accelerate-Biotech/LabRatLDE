package frontmatter

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name, in, body string
		meta           map[string]any
	}{
		{"none", "# Title\nbody", "# Title\nbody", map[string]any{}},
		{"basic", "---\nprotocol: qpcr\nversion: 3\n---\n# Steps\n", "# Steps\n", map[string]any{"protocol": "qpcr", "version": 3}},
		{"crlf", "---\r\nprotocol: hplc\r\n---\r\nbody", "body", map[string]any{"protocol": "hplc"}},
		{"empty block", "---\n---\nbody", "body", map[string]any{}},
		{"no body", "---\nk: v\n---", "", map[string]any{"k": "v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(doc.Body) != tt.body {
				t.Errorf("body = %q, want %q", doc.Body, tt.body)
			}
			if len(doc.Meta) != len(tt.meta) {
				t.Fatalf("meta = %v, want %v", doc.Meta, tt.meta)
			}
			for k, v := range tt.meta {
				if doc.Meta[k] != v {
					t.Errorf("meta[%s] = %v, want %v", k, doc.Meta[k], v)
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("---\nk: v\nno close")); !errors.Is(err, ErrUnterminated) {
		t.Errorf("got %v, want ErrUnterminated", err)
	}
	if _, err := Parse([]byte("---\nk: [unclosed\n---\n")); err == nil {
		t.Error("invalid YAML should fail")
	}
}
