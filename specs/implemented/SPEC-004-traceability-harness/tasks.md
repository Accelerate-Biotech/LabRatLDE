# SPEC-004 tasks: Traceability harness

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

No migrations and no schema changes. Run `docker compose up -d` before any task that says `make validate` or `labrat iq`.

## Phase 1: Validation tool (`tools/validate`)

- [x] 1.1 Test: verdicts, skips and missing results (SPEC-004-R02, R03)
  - Requirements: SPEC-004-R02, SPEC-004-R03
  - Files (all new):
    - `tools/validate/validate_test.go` (`package main`)
    - `tools/validate/testdata/specs/SPEC-900-fixture/requirements.md`
    - `tools/validate/testdata/specs/SPEC-901-draft/requirements.md`
    - `tools/validate/testdata/src/fake/fake_test.go`
    - `tools/validate/testdata/src/.hidden/hidden_test.go`
  - Do:
    - Fixture specs: each has a header table with a `| **Status** | … |` row and a requirement table in the same format as the real specs (`| ID | Requirement | Test | Source |`).
      - `SPEC-900-fixture` is `Approved`, with `SPEC-900-R01` `yes`, `SPEC-900-R02` `yes` and `SPEC-900-R03` `no`.
      - `SPEC-901-draft` is `Draft`, with `SPEC-901-R01` `yes`.
    - `fake/fake_test.go`, `package fake`:
      - `// SPEC-900-R01` above `func TestCitesR01(t *testing.T) {}`;
      - `func TestCitesR01Inside(t *testing.T)` with a `t.Run("sub", …)` whose body holds the comment `// SPEC-900-R01`;
      - `// SPEC-999-R01` above `func TestUnknown(t *testing.T) {}`;
      - `// SPEC-900-R02` above `func helper() {}`, which must not count.
    - `.hidden/hidden_test.go`: `// SPEC-900-R02` above `func TestHidden(t *testing.T) {}`, which must not count.
    - Module for the scan: `"example.com/fx"`. Result keys look like `"example.com/fx/fake.TestCitesR01"`.
    - Write two test functions. Each loads `parseSpecs("testdata/specs")` and `scanTests("testdata/src", "example.com/fx")`, then calls `buildReport(reqs, cites, results, true, false)`, and checks `verdict` per ID, `Result`, and which IDs appear in `Requirements`.
    - `TestVerdicts`, comment `// SPEC-004-R02`, table-driven:
      - (a) All three fake tests `pass`. `SPEC-900-R01` is `VERIFIED`. `SPEC-900-R02` is `NOT VERIFIED`, because neither the helper nor the hidden file counts. Neither `SPEC-900-R03` nor `SPEC-901-R01` is listed. `UnknownReferences` holds `SPEC-999-R01`. `Result` is `FAIL`.
      - (b) As (a), but `TestCitesR01Inside` is `fail`, so `SPEC-900-R01` is `NOT VERIFIED`.
      - (c) Delete `SPEC-900-R02` from `reqs` and drop the `SPEC-999-R01` citation. `Result` is `PASS`, even though the Draft `SPEC-901-R01` is uncited.
      - (d) As (c), but `buildReport(…, false, false)` (go test failed), so `Result` is `FAIL`.
      - (e) As (c), but `dirty=true`, so `Result` is `FAIL`.
    - `TestSkippedOrMissingIsNotPassed`, comment `// SPEC-004-R03`, based on (c):
      - `TestCitesR01` is `skip`, so `SPEC-900-R01` is `NOT VERIFIED`.
      - `TestCitesR01` is absent from `results`, so `SPEC-900-R01` is `NOT VERIFIED` and that test's `result` is `"none"`.
    - Use the field and type names from `design.md` §2. Use `report.Requirements[i].Verdict`, `.Tests[j].Result` and `report.UnknownReferences[i].ID`.
  - Verify: `go vet ./tools/validate/` fails only because `parseSpecs`, `scanTests` and `buildReport` are undefined.
- [x] 1.2 Spec parser and test scanner
  - Requirements: SPEC-004-R01, SPEC-004-R04
  - Files: `tools/validate/specs.go`, `tools/validate/cites.go` (new)
  - Do:
    - Types `requirement` and `citation`, and functions `parseSpecs` and `scanTests`, exactly as in `design.md` §2 "Specs" and "Citations".
    - Standard library only: `bufio`, `go/parser`, `go/token`, `go/ast`, `io/fs`, `path/filepath`, `regexp`.
    - In `scanTests`:
      - Skip a directory when its name starts with `.` or `_`, or is `testdata` or `vendor`. Never skip `root` itself, even when it is `testdata/src`.
      - Build `Package` with `filepath.ToSlash`.
      - Store `File` relative to `root`, slash-separated.
    - Also add `readModule(root string) (string, error)`, which returns the path after `module ` in `<root>/go.mod`.
  - Verify: `go vet ./tools/validate/` reports only the missing `buildReport`.
- [x] 1.3 Verdicts and the `go test` runner
  - Requirements: SPEC-004-R01, SPEC-004-R02, SPEC-004-R03, SPEC-004-R04
  - Files: `tools/validate/report.go` (new)
  - Do:
    - Types `testResult`, `environment`, `testRow`, `reqRow`, `unknownRef`, `summary` and `report`, with the JSON tags of the `report.json` example in `design.md` §2. `report.Environment` is left zero by `buildReport` and filled by `main`.
    - `buildReport(reqs map[string]requirement, cites []citation, results map[string]testResult, goTestPassed, dirty bool) report`: exactly the "Verdicts" rules. Sort rows by ID, tests by package then name, and unknown references by ID then test. Set `Format` to `"labrat-validation/1"` and fill `Summary`.
    - `goTest(dir string) (map[string]testResult, bool, error)`, as in the design's "Test run" paragraph. Use `exec.Command("go", "test", "-json", "-count=1", "./...")` with `Dir = dir` and `Stderr = os.Stderr`, and decode stdout with `json.NewDecoder`. An `*exec.ExitError` means `false, nil`.
  - Verify: `go test ./tools/validate/` passes both 1.1 tests without editing them. `gofmt -l . && go vet ./...` is clean.
- [x] 1.4 Report writer, `main`, `make validate`
  - Requirements: SPEC-004-R01, SPEC-004-R05, SPEC-004-R06, SPEC-004-R07, SPEC-004-R10, SPEC-004-R11
  - Files: `tools/validate/main.go` (new), `tools/validate/report.go` (add `writeReport`), `Makefile`, `.gitignore`
  - Do:
    - `writeReport(dir string, r report) (sha string, err error)`:
      - Write `report.json`, `report.sha256` and `report.md` exactly as in `design.md` §2 "Output".
      - Hash the exact bytes written to `report.json`.
      - In `report.md`, shorten package paths by trimming the module prefix. Keep the module in an unexported field set by `main`, or pass it in as a parameter.
    - `main`: the six-step "Run sequence" from the design. Flags use the `flag` package.
      - An empty `--operator` prints `validate: --operator is required` and exits 2.
      - Run git with `exec.Command("git", …)` and `Dir = root`.
      - Connect with `pgx.Connect(ctx, os.Getenv("LDE_TEST_DATABASE_URL"))`, then close.
      - Errors (as opposed to a FAIL result) print `validate: <err>` and exit 2.
    - `Makefile`:
      - Add `validate` to `.PHONY`.
      - Add the target from design §2 after `test-integration`, with `OPERATOR ?=` (empty) near `TEST_DB`.
    - `.gitignore`: add `/validation/`.
  - Verify:
    - `gofmt -l . && go vet ./...` is clean.
    - `make validate OPERATOR="Test Operator"` on a clean checkout of this branch: commit first, or expect the dirty banner. It writes `validation/<stamp>/`, and `sha256sum -c report.sha256` succeeds in that folder. `report.md` lists every `Test: yes` requirement of SPEC-001 to SPEC-003 as `VERIFIED`,. `SPEC-004-R02` and `SPEC-004-R03` are listed (the spec is Approved) and `VERIFIED`. `report.md` ends with the sign-off table.
    - `LDE_TEST_DATABASE_URL= go run ./tools/validate --operator x` exits 2 before running tests.
    - With an uncommitted edit, the report shows `NOT FOR FILING: uncommitted changes` and the exit code is 1.

## Phase 2: Installation qualification (`labrat iq`)

- [x] 2.1 Export the embedded migration list
  - Requirements: SPEC-004-R08
  - Files: `internal/storage/migrate.go`
  - Do:
    - Add `func MigrationVersions() ([]string, error)`. It globs `migrations/*.sql` from `migrationFS`, sorts the names, and returns them without the `migrations/` prefix and `.sql` suffix (`"0001_core"`, …).
    - In `Migrate`, replace the glob, sort and trim with a loop over `MigrationVersions()`. It reads each file as `"migrations/" + version + ".sql"`.
    - Behaviour is unchanged. Touch no migration file.
  - Verify: `make test-integration` passes. `gofmt -l . && go vet ./...` is clean.
  - Integrity: yes
- [x] 2.2 `labrat iq` subcommand
  - Requirements: SPEC-004-R08, SPEC-004-R09
  - Files: `cmd/server/iq.go` (new), `cmd/server/main.go` (dispatch only)
  - Do:
    - In `main()`, if `len(os.Args) > 1 && os.Args[1] == "iq"`, run `runIQ(os.Args[2:])` instead of `run()`, with the same error printing and `os.Exit(1)`.
    - `runIQ(args []string) error` in `iq.go`:
      - It takes no flags. Any extra argument returns `fmt.Errorf("iq: unexpected argument %q", args[0])`.
      - It loads config, then `storage.Open` with a 30 s connect timeout. It never calls `Migrate` or `EnsureTenant`.
      - It runs the five checks in the order of the `design.md` §2 table, with that SQL and the `iqTriggers` list verbatim.
      - Each check prints its lines to stdout with `fmt.Printf("%-4s  %-10s  %s\n", "PASS"|"FAIL", check, detail)`.
      - `binary` prints `version <version>  sha256 <hex>`.
      - At the end it returns `fmt.Errorf("iq: %d check(s) failed", n)` if anything failed, `nil` otherwise.
    - Keep every query in `iq.go`, next to the check that runs it.
  - Verify:
    - `gofmt -l . && go vet ./...` is clean.
    - With compose up and the server run once (so migrations are applied), `go run ./cmd/server iq` prints only PASS lines and exits 0.
    - Run `docker compose exec -T postgres psql -U lde -d lde -c "ALTER TABLE runs DISABLE TRIGGER runs_protocol_pinned; ALTER TABLE experiments NO FORCE ROW LEVEL SECURITY;"`. `iq` must now print a FAIL for each change and exit 1.
    - Restore with `ENABLE TRIGGER runs_protocol_pinned` and `FORCE ROW LEVEL SECURITY`, and confirm `iq` is all PASS again.
  - Integrity: yes

## Phase 3: Docs

- [x] 3.1 README: validation section
  - Requirements: SPEC-004-R01, SPEC-004-R08
  - Files: `README.md`
  - Do: add a short "Validation (IQ/OQ)" section next to the existing commands. It covers:
    - `go run ./cmd/server iq` (or `labrat iq`) for IQ;
    - `make validate OPERATOR="Name"` for OQ: where the report lands, what makes it FAIL, and `sha256sum -c report.sha256`;
    - that the report must come from a clean, committed tree to be filed.
    Keep it under 20 lines. Match the README's existing tone.
  - Verify: the commands in the section run as written.

## Phase 4: Verify and close out

- [x] 4.1 `verifier` passes the whole spec
- [x] 4.2 `uat-planner` writes [uat.md](uat.md)
- [x] 4.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated
- [x] 4.4 Summary commit written as the PR description

## Phase 5: Reframe (added 2026-10-26, after implementation)

- [x] 5.1 Rename to "Traceability harness" and state the scope in the report
  - Requirements: SPEC-004-R12
  - Files: this spec folder (renamed to `SPEC-004-traceability-harness/`), `tools/validate/report.go`, `tools/validate/main.go` (comment), `README.md`, `specs/ROADMAP.md`
  - Do: retitle the spec; print the R12 scope line under the `report.md` title, which becomes `# LabRat traceability report`; drop "fit for filing" from the README; reserve SPEC-020 for validation documentation in the roadmap.
  - Verify: `gofmt -l . && go vet ./... && go test ./tools/validate/`; `make validate` output starts with the new title and scope line.
