package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// testResult is the outcome of one test from go test -json.
type testResult struct {
	Result  string
	Elapsed float64
}

type environment struct {
	Operator        string    `json:"operator"`
	GitCommit       string    `json:"git_commit"`
	GitDirty        bool      `json:"git_dirty"`
	GoVersion       string    `json:"go_version"`
	OSArch          string    `json:"os_arch"`
	PostgresVersion string    `json:"postgres_version"`
	Migrations      []string  `json:"migrations"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

type testRow struct {
	Package  string  `json:"package"`
	Name     string  `json:"name"`
	Result   string  `json:"result"`
	ElapsedS float64 `json:"elapsed_s"`
}

type reqRow struct {
	ID         string    `json:"id"`
	Spec       string    `json:"spec"`
	SpecStatus string    `json:"spec_status"`
	Tests      []testRow `json:"tests"`
	Verdict    string    `json:"verdict"`
}

type unknownRef struct {
	ID      string `json:"id"`
	Package string `json:"package"`
	Test    string `json:"test"`
	File    string `json:"file"`
}

type summary struct {
	Verified    int `json:"verified"`
	NotVerified int `json:"not_verified"`
	Total       int `json:"total"`
}

type report struct {
	Format            string       `json:"format"`
	Result            string       `json:"result"`
	Environment       environment  `json:"environment"`
	GoTestPassed      bool         `json:"go_test_passed"`
	Requirements      []reqRow     `json:"requirements"`
	UnknownReferences []unknownRef `json:"unknown_references"`
	Summary           summary      `json:"summary"`
}

// buildReport joins requirements, citations and test results into the verdicts.
// Environment is left zero for the caller to fill.
func buildReport(reqs map[string]requirement, cites []citation, results map[string]testResult, goTestPassed, dirty bool) report {
	rep := report{
		Format:            "labrat-validation/1",
		GoTestPassed:      goTestPassed,
		Requirements:      []reqRow{},
		UnknownReferences: []unknownRef{},
	}

	var ids []string
	for id, r := range reqs {
		if r.TestYes && (r.SpecStatus == "Approved" || r.SpecStatus == "Implemented") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	for _, id := range ids {
		r := reqs[id]
		row := reqRow{ID: id, Spec: r.Spec, SpecStatus: r.SpecStatus, Tests: []testRow{}}
		for _, c := range cites {
			if c.ID != id {
				continue
			}
			tr := testRow{Package: c.Package, Name: c.Test, Result: "none"}
			if res, ok := results[c.Package+"."+c.Test]; ok {
				tr.Result = res.Result
				tr.ElapsedS = res.Elapsed
			}
			row.Tests = append(row.Tests, tr)
		}
		sort.Slice(row.Tests, func(i, j int) bool {
			a, b := row.Tests[i], row.Tests[j]
			if a.Package != b.Package {
				return a.Package < b.Package
			}
			return a.Name < b.Name
		})

		row.Verdict = "VERIFIED"
		if len(row.Tests) == 0 {
			row.Verdict = "NOT VERIFIED"
		}
		for _, tr := range row.Tests {
			if tr.Result != "pass" {
				row.Verdict = "NOT VERIFIED"
			}
		}
		if row.Verdict == "VERIFIED" {
			rep.Summary.Verified++
		} else {
			rep.Summary.NotVerified++
		}
		rep.Requirements = append(rep.Requirements, row)
	}
	rep.Summary.Total = len(rep.Requirements)

	for _, c := range cites {
		if _, ok := reqs[c.ID]; !ok {
			rep.UnknownReferences = append(rep.UnknownReferences, unknownRef{
				ID: c.ID, Package: c.Package, Test: c.Test, File: c.File,
			})
		}
	}
	sort.SliceStable(rep.UnknownReferences, func(i, j int) bool {
		a, b := rep.UnknownReferences[i], rep.UnknownReferences[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Test < b.Test
	})

	rep.Result = "PASS"
	if rep.Summary.NotVerified > 0 || len(rep.UnknownReferences) > 0 || !goTestPassed || dirty {
		rep.Result = "FAIL"
	}
	return rep
}

// goTest runs the module's tests in dir and returns one result per top-level test,
// keyed by Package + "." + Test, and whether go test exited 0.
func goTest(dir string) (map[string]testResult, bool, error) {
	cmd := exec.Command("go", "test", "-json", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}

	type event struct {
		Action  string
		Package string
		Test    string
		Elapsed float64
	}
	results := map[string]testResult{}
	dec := json.NewDecoder(stdout)
	for {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			break
		}
		if ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			results[ev.Package+"."+ev.Test] = testResult{Result: ev.Action, Elapsed: ev.Elapsed}
		}
	}
	// Drain anything the decoder stopped on so go test can exit.
	_, _ = io.Copy(io.Discard, stdout)

	err = cmd.Wait()
	if _, ok := err.(*exec.ExitError); ok {
		return results, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return results, true, nil
}

// writeReport writes report.json, report.sha256 and report.md into dir and
// returns the lowercase hex SHA-256 of the report.json bytes. Package paths in
// report.md are shown relative to module.
func writeReport(dir string, r report, module string) (sha string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	js, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	js = append(js, '\n')
	sum := sha256.Sum256(js)
	sha = hex.EncodeToString(sum[:])

	if err := os.WriteFile(filepath.Join(dir, "report.json"), js, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.sha256"), []byte(sha+"  report.json\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(markdownReport(r, sha, module)), 0o644); err != nil {
		return "", err
	}
	return sha, nil
}

// markdownReport renders the human-readable report, sections in filing order.
func markdownReport(r report, sha, module string) string {
	var b strings.Builder
	short := func(pkg string) string {
		if pkg == module {
			return "."
		}
		return strings.TrimPrefix(pkg, module+"/")
	}
	env := r.Environment

	fmt.Fprintln(&b, "# LabRat validation report (OQ)")
	fmt.Fprintln(&b)
	if env.GitDirty {
		fmt.Fprintln(&b, "**NOT FOR FILING: uncommitted changes**")
		fmt.Fprintln(&b)
	}
	if !r.GoTestPassed {
		fmt.Fprintln(&b, "**go test reported failures**")
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "**Result: %s**\n\n", r.Result)
	fmt.Fprintf(&b, "report.json SHA-256: `%s`\n\n", sha)

	migrations := "none"
	if len(env.Migrations) > 0 {
		migrations = strings.Join(env.Migrations, ", ")
	}
	dirty := "no"
	if env.GitDirty {
		dirty = "yes"
	}
	fmt.Fprintln(&b, "## Environment")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Field | Value |")
	fmt.Fprintln(&b, "|---|---|")
	fmt.Fprintf(&b, "| Operator | %s |\n", env.Operator)
	fmt.Fprintf(&b, "| Git commit | %s |\n", env.GitCommit)
	fmt.Fprintf(&b, "| Uncommitted changes | %s |\n", dirty)
	fmt.Fprintf(&b, "| Go version | %s |\n", env.GoVersion)
	fmt.Fprintf(&b, "| OS/arch | %s |\n", env.OSArch)
	fmt.Fprintf(&b, "| PostgreSQL version | %s |\n", env.PostgresVersion)
	fmt.Fprintf(&b, "| Migrations | %s |\n", migrations)
	fmt.Fprintf(&b, "| Started (UTC) | %s |\n", env.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "| Finished (UTC) | %s |\n", env.FinishedAt.Format(time.RFC3339))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Traceability matrix")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Requirement | Spec (status) | Tests | Verdict |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, row := range r.Requirements {
		var tests []string
		for _, t := range row.Tests {
			tests = append(tests, fmt.Sprintf("%s.%s: %s", short(t.Package), t.Name, t.Result))
		}
		cell := "none"
		if len(tests) > 0 {
			cell = strings.Join(tests, "<br>")
		}
		fmt.Fprintf(&b, "| %s | %s (%s) | %s | %s |\n", row.ID, row.Spec, row.SpecStatus, cell, row.Verdict)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Unknown references")
	fmt.Fprintln(&b)
	if len(r.UnknownReferences) == 0 {
		fmt.Fprintln(&b, "None.")
	} else {
		fmt.Fprintln(&b, "| ID | Package | Test | File |")
		fmt.Fprintln(&b, "|---|---|---|---|")
		for _, u := range r.UnknownReferences {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", u.ID, short(u.Package), u.Test, u.File)
		}
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Sign-off")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Role | Name | Signature | Date |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	fmt.Fprintln(&b, "| Executed by | | | |")
	fmt.Fprintln(&b, "| Reviewed by | | | |")
	fmt.Fprintln(&b, "| QA approved | | | |")
	return b.String()
}
