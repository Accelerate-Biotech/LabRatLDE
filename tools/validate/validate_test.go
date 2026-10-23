package main

import (
	"strings"
	"testing"
)

const fxPkg = "example.com/fx/fake."

// loadFixture reads the fixture specs and the fixture test citations.
func loadFixture(t *testing.T) (map[string]requirement, []citation) {
	t.Helper()
	reqs, err := parseSpecs("testdata/specs")
	if err != nil {
		t.Fatal(err)
	}
	cites, err := scanTests("testdata/src", "example.com/fx")
	if err != nil {
		t.Fatal(err)
	}
	return reqs, cites
}

// passingResults returns a result for every fixture test, all passing.
func passingResults() map[string]testResult {
	return map[string]testResult{
		fxPkg + "TestCitesR01":       {Result: "pass"},
		fxPkg + "TestCitesR01Inside": {Result: "pass"},
		fxPkg + "TestUnknown":        {Result: "pass"},
	}
}

// dropR02 removes SPEC-900-R02 from the requirements and the SPEC-999 citation,
// so the only uncited requirement left is the Draft SPEC-901-R01.
func dropR02(reqs map[string]requirement, cites []citation) []citation {
	delete(reqs, "SPEC-900-R02")
	var kept []citation
	for _, c := range cites {
		if c.ID != "SPEC-999-R01" {
			kept = append(kept, c)
		}
	}
	return kept
}

// SPEC-004-R02
func TestVerdicts(t *testing.T) {
	cases := []struct {
		name     string
		drop     bool
		failed   string
		goPassed bool
		dirty    bool
		verdicts map[string]string
		unknown  []string
		result   string
	}{
		{
			name:     "a: all pass, R02 uncited, unknown reference",
			goPassed: true,
			verdicts: map[string]string{"SPEC-900-R01": "VERIFIED", "SPEC-900-R02": "NOT VERIFIED"},
			unknown:  []string{"SPEC-999-R01"},
			result:   "FAIL",
		},
		{
			name:     "b: one citing test fails",
			failed:   fxPkg + "TestCitesR01Inside",
			goPassed: true,
			verdicts: map[string]string{"SPEC-900-R01": "NOT VERIFIED", "SPEC-900-R02": "NOT VERIFIED"},
			unknown:  []string{"SPEC-999-R01"},
			result:   "FAIL",
		},
		{
			name:     "c: every requirement verified, Draft uncited",
			drop:     true,
			goPassed: true,
			verdicts: map[string]string{"SPEC-900-R01": "VERIFIED"},
			result:   "PASS",
		},
		{
			name:     "d: go test failed",
			drop:     true,
			goPassed: false,
			verdicts: map[string]string{"SPEC-900-R01": "VERIFIED"},
			result:   "FAIL",
		},
		{
			name:     "e: tree dirty",
			drop:     true,
			goPassed: true,
			dirty:    true,
			verdicts: map[string]string{"SPEC-900-R01": "VERIFIED"},
			result:   "FAIL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs, cites := loadFixture(t)
			results := passingResults()
			if tc.drop {
				cites = dropR02(reqs, cites)
			}
			if tc.failed != "" {
				results[tc.failed] = testResult{Result: "fail"}
			}
			rep := buildReport(reqs, cites, results, tc.goPassed, tc.dirty)

			got := map[string]string{}
			for _, r := range rep.Requirements {
				got[r.ID] = r.Verdict
			}
			if len(got) != len(tc.verdicts) {
				t.Errorf("requirements = %v, want %v", got, tc.verdicts)
			}
			for id, want := range tc.verdicts {
				if got[id] != want {
					t.Errorf("%s verdict = %q, want %q", id, got[id], want)
				}
			}
			var unknown []string
			for _, u := range rep.UnknownReferences {
				unknown = append(unknown, u.ID)
			}
			if strings.Join(unknown, ",") != strings.Join(tc.unknown, ",") {
				t.Errorf("unknown references = %v, want %v", unknown, tc.unknown)
			}
			if rep.Result != tc.result {
				t.Errorf("result = %q, want %q", rep.Result, tc.result)
			}
		})
	}
}

// SPEC-004-R03
func TestSkippedOrMissingIsNotPassed(t *testing.T) {
	cases := []struct {
		name    string
		present bool
		result  testResult
		want    string
	}{
		{name: "skipped", present: true, result: testResult{Result: "skip"}, want: "skip"},
		{name: "no result", present: false, want: "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs, cites := loadFixture(t)
			cites = dropR02(reqs, cites)
			results := passingResults()
			if tc.present {
				results[fxPkg+"TestCitesR01"] = tc.result
			} else {
				delete(results, fxPkg+"TestCitesR01")
			}
			rep := buildReport(reqs, cites, results, true, false)

			var found bool
			for _, r := range rep.Requirements {
				if r.ID != "SPEC-900-R01" {
					continue
				}
				found = true
				if r.Verdict != "NOT VERIFIED" {
					t.Errorf("SPEC-900-R01 verdict = %q, want NOT VERIFIED", r.Verdict)
				}
				var hit bool
				for _, tr := range r.Tests {
					if tr.Name == "TestCitesR01" && tr.Result == tc.want {
						hit = true
					}
				}
				if !hit {
					t.Errorf("TestCitesR01 result %q not found in %+v", tc.want, r.Tests)
				}
			}
			if !found {
				t.Error("SPEC-900-R01 missing from report")
			}
		})
	}
}
