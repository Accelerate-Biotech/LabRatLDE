package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
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
