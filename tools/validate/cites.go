package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// citation is one requirement ID cited by one top-level test.
type citation struct {
	ID, Package, Test, File string
}

var specRef = regexp.MustCompile(`SPEC-\d{3}-R\d{2}`)

// scanTests finds every SPEC-NNN-RNN cited by a top-level Test function in the
// *_test.go files under root. module is the module path of root.
func scanTests(root, module string) ([]citation, error) {
	var cites []citation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found, err := scanFile(root, path, module)
		if err != nil {
			return err
		}
		cites = append(cites, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cites, nil
}

// scanFile returns the citations made by the top-level Test functions in one file.
func scanFile(root, path, module string) ([]citation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	file := filepath.ToSlash(rel)
	pkg := module
	if dir := filepath.ToSlash(filepath.Dir(rel)); dir != "." {
		pkg = module + "/" + dir
	}

	var cites []citation
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		var groups []*ast.CommentGroup
		if fn.Doc != nil {
			groups = append(groups, fn.Doc)
		}
		for _, cg := range f.Comments {
			if cg.Pos() >= fn.Body.Pos() && cg.End() <= fn.Body.End() {
				groups = append(groups, cg)
			}
		}
		seen := map[string]bool{}
		for _, cg := range groups {
			for _, c := range cg.List {
				for _, id := range specRef.FindAllString(c.Text, -1) {
					if seen[id] {
						continue
					}
					seen[id] = true
					cites = append(cites, citation{ID: id, Package: pkg, Test: fn.Name.Name, File: file})
				}
			}
		}
	}
	return cites, nil
}
