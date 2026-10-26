package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// requirement is one row of a spec's requirements table.
type requirement struct {
	ID, Spec, SpecStatus string
	TestYes              bool
}

// parseSpecs reads every requirements.md under root (except TEMPLATE) and
// returns the requirement rows keyed by ID.
func parseSpecs(root string) (map[string]requirement, error) {
	reqs := map[string]requirement{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "TEMPLATE" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "requirements.md" {
			return nil
		}
		return parseSpecFile(path, reqs)
	})
	if err != nil {
		return nil, err
	}
	return reqs, nil
}

// parseSpecFile adds the requirement rows of one requirements.md to reqs.
func parseSpecFile(path string, reqs map[string]requirement) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var status string
	var rows []requirement
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		cells := strings.Split(line, "|")
		switch {
		case strings.HasPrefix(line, "| **Status** |"):
			status = strings.TrimSpace(cells[2])
		case strings.HasPrefix(line, "| SPEC-"):
			if len(cells) < 4 {
				return fmt.Errorf("%s: malformed requirement row: %s", path, line)
			}
			id := strings.TrimSpace(cells[1])
			if len(id) < 8 {
				return fmt.Errorf("%s: malformed requirement ID %q", path, id)
			}
			rows = append(rows, requirement{
				ID:      id,
				Spec:    id[:8],
				TestYes: strings.TrimSpace(cells[len(cells)-3]) == "yes",
			})
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if status == "" {
		return fmt.Errorf("%s: no **Status** row", path)
	}
	for _, r := range rows {
		if _, dup := reqs[r.ID]; dup {
			return fmt.Errorf("%s: duplicate requirement %s", path, r.ID)
		}
		r.SpecStatus = status
		reqs[r.ID] = r
	}
	return nil
}

// readModule returns the module path from the module line of <root>/go.mod.
func readModule(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if mod, ok := strings.CutPrefix(sc.Text(), "module "); ok {
			return strings.TrimSpace(mod), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s/go.mod: no module line", root)
}
