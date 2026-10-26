# SPEC-004 design: Traceability harness

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one Go program, `tools/validate` (`package main`, four small files). It:
  - reads the requirement tables in `specs/**/requirements.md`;
  - scans `*_test.go` with `go/parser` for `SPEC-NNN-Rnn` comments on top-level test functions;
  - runs `go test -json -count=1 ./...`;
  - joins the three and writes `report.md`, `report.json` and `report.sha256`.
  It uses the standard library plus `pgx`, which is already a dependency, for two read-only queries (server version, applied migrations). `labrat iq` is a subcommand of the server binary: one new file, `cmd/server/iq.go`, of catalog queries.
- Not doing: a test-management database, PDF generation, a web dashboard, `--json` output for `iq`, a raw `go test` log in the report folder, or flagging tests that cite `Test: no` requirements.

## 2. Data and interfaces

### `tools/validate`

| File | Contents |
|---|---|
| `main.go` | flags, the run sequence, exit code |
| `specs.go` | `parseSpecs(root string) (map[string]requirement, error)` |
| `cites.go` | `scanTests(root, module string) ([]citation, error)` |
| `report.go` | `goTest`, `buildReport`, `writeReport` |

```go
type requirement struct{ ID, Spec, SpecStatus string; TestYes bool }
type citation    struct{ ID, Package, Test, File string } // Package is the import path
type testResult  struct{ Result string; Elapsed float64 }  // "pass" | "fail" | "skip"
```

**Specs** (`parseSpecs`). Walk `root` for files named `requirements.md`. Skip the `TEMPLATE` directory. For each file:
- The spec status is the cell after `| **Status** |`.
- Each line starting with `| SPEC-` is a requirement row. Split it on `|`. The ID is the first cell. `TestYes` is true when the third cell from the end, trimmed, is `yes`. Counting from the end keeps a stray `|` in the requirement text harmless.
- The spec ID is the ID's first eight characters (`SPEC-004`).
- A duplicate ID is an error.

**Citations** (`scanTests`). Walk `root`. Skip directories whose name starts with `.` or `_`, `testdata` and `vendor`, exactly as `go test ./...` does. This matters: `.claude/worktrees/` holds full copies of the repo's tests. For each `*_test.go`, parse it with `parser.ParseComments`. For each top-level `FuncDecl` whose name starts with `Test`, gather every `SPEC-\d{3}-R\d{2}` match from:
- its doc comment;
- any comment inside the function body, by position. Subtests count for their enclosing top-level test.
Comments anywhere else (helpers, file headers) don't count. `Package` is `module + "/" + the directory relative to root` (slash-separated; just `module` at the root). The module comes from the `module` line of `go.mod`.

**Test run** (`goTest(dir string) (map[string]testResult, bool, error)`). Run `go test -json -count=1 ./...` in `dir`. The child gets the tool's environment, including `LDE_TEST_DATABASE_URL`. Pass its stderr through to the tool's stderr. Decode stdout as a stream of `{Action, Package, Test, Elapsed}`. For events whose `Test` has no `/` and whose `Action` is `pass`, `fail` or `skip`, store the result under the key `Package + "." + Test`. The bool reports whether `go test` exited 0. A non-zero exit is not an `error`; only failing to start is.

**Verdicts** (`buildReport`), per R01–R04:
- The report lists exactly the `TestYes` requirements of specs with status `Approved` or `Implemented`, sorted by ID.
- Each listed requirement gets its citing tests, sorted by package then name. A test with no result entry gets `result: "none"`.
- A requirement is `VERIFIED` when it has at least one citing test and every citing test has `result: "pass"`. Otherwise it is `NOT VERIFIED`.
- A citation whose ID is not a key of the `parseSpecs` map (in any spec, at any status) goes under `unknown_references`.
- `result` is `PASS` when all of these hold, and `FAIL` otherwise:
  - nothing is `NOT VERIFIED`;
  - `unknown_references` is empty;
  - `go test` exited 0;
  - the tree is clean.

**Run sequence** (`main`). Flags: `--operator` (required, non-empty), `--root` (default `.`), `--out` (default `validation`).
1. If `LDE_TEST_DATABASE_URL` is empty, print `validate: LDE_TEST_DATABASE_URL is not set` and exit 2 before anything else (R05).
2. Record `started_at`.
3. Read git state:
   - the commit is `git rev-parse HEAD`;
   - the tree is dirty when `git status --porcelain` prints anything.
4. Parse the specs (`<root>/specs`), scan the tests (`<root>`) and run `goTest(root)`. Record `finished_at`.
5. Connect with `pgx.Connect` and read:
   - `SHOW server_version`;
   - `SELECT version FROM schema_migrations ORDER BY version`.
   This happens after the tests, which migrate the database.
6. Write the three files to `<out>/<stamp>/`, where `<stamp>` is `started_at` formatted with Go layout `20060102T150405Z`. Then print the folder, the summary line and `result`. Exit 1 when `result` is `FAIL`, 0 otherwise.

### Output

`make validate OPERATOR="J. Doe"` → `validation/<timestamp>/{report.json, report.sha256, report.md}`. `validation/` is git-ignored.

`report.json`, written with `json.MarshalIndent(…, "", "  ")` plus a trailing newline:

```json
{
  "format": "labrat-validation/1",
  "result": "FAIL",
  "environment": {
    "operator": "J. Doe", "git_commit": "8c20bad…", "git_dirty": false,
    "go_version": "go1.27.0", "os_arch": "linux/amd64",
    "postgres_version": "16.4", "migrations": ["0001_core", "…"],
    "started_at": "2026-10-22T15:04:05Z", "finished_at": "2026-10-22T15:05:10Z"
  },
  "go_test_passed": true,
  "requirements": [
    {"id": "SPEC-001-R02", "spec": "SPEC-001", "spec_status": "Implemented",
     "tests": [{"package": "github.com/Accelerate-Biotech/LabRatLDE/internal/audit", "name": "TestDigestGolden", "result": "pass", "elapsed_s": 0.01}],
     "verdict": "VERIFIED"}
  ],
  "unknown_references": [{"id": "SPEC-099-R01", "package": "…", "test": "TestX", "file": "internal/x/x_test.go"}],
  "summary": {"verified": 30, "not_verified": 1, "total": 31}
}
```

`go_version` and `os_arch` come from `runtime.Version()` and `runtime.GOOS + "/" + runtime.GOARCH`. Under `go run` that is the toolchain running the tests. Times are UTC, RFC 3339.

`report.sha256` holds `<lowercase hex>  report.json\n`, so `sha256sum -c report.sha256` checks it (R07).

`report.md`, in this order:
1. `# LabRat traceability report`, followed by the scope line (R12): `*Test-traceability evidence only. Not by itself sufficient for a regulatory filing: see specs/ROADMAP.md (SPEC-020).*`
2. Banner lines, only when they apply (R10):
   - `**NOT FOR FILING: uncommitted changes**` when the tree is dirty;
   - `**go test reported failures**` when it didn't exit 0.
3. `**Result: PASS|FAIL**`, then `report.json SHA-256: \`<hex>\``.
4. An `## Environment` table of every `environment` field.
5. `## Traceability matrix`, with columns `Requirement | Spec (status) | Tests | Verdict`. Tests are listed as `Package.TestName: result`, joined with `<br>`. Package is shortened to the part after the module path.
6. `## Unknown references`: a table, or `None.`
7. `## Sign-off` (R11): a table with columns `Role | Name | Signature | Date` and rows `Executed by`, `Reviewed by`, `QA approved`, all cells blank.

### `make validate`

```make
validate: ## OQ: run the suite against the database and write a stamped traceability report
	LDE_TEST_DATABASE_URL="$(TEST_DB)" go run ./tools/validate --operator "$(OPERATOR)"
```

### `labrat iq`

`main()` dispatches `labrat iq` to `runIQ(os.Args[2:])` before the server starts. `runIQ` lives in `cmd/server/iq.go`:
- It loads config with `config.Load()` and opens `storage.Open(ctx, cfg.DatabaseURL)`.
- It never calls `Migrate` and runs only catalog reads, so it emits no audit event.
- It prints one line per check, `PASS  <check>  <detail>` or `FAIL  <check>  <detail>`, with one FAIL line per failing object (R09).
- It returns an error `iq: N check(s) failed` when anything failed, so `main` exits 1.

| Check | How |
|---|---|
| `binary` | Always passes. Prints `version` and the SHA-256 of `os.Executable()` |
| `migrations` | `storage.MigrationVersions()` (new, exported; `Migrate` uses it too) vs `SELECT version FROM schema_migrations`. FAIL for each embedded version that isn't applied, and each applied version that isn't embedded (a newer binary migrated this DB) |
| `rls` | `pg_class` rows in schema `public` with `relkind = 'r'` that are `tenants` or have a live `tenant_id` column. FAIL each that lacks `relrowsecurity` or `relforcerowsecurity` |
| `triggers` | Each name in `iqTriggers` must exist in `pg_trigger` (`NOT tgisinternal`) with `tgenabled <> 'D'`. FAIL also for any other non-internal trigger on a `public` table with `tgenabled = 'D'` |
| `lde_app` | `SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'lde_app'`. FAIL if the role is missing or either flag is true |

`iqTriggers` is the integrity triggers the migrations create today:

```go
// Integrity triggers labrat iq requires. A spec that adds one adds it here.
var iqTriggers = []string{
	"audit_events_link", "audit_events_append_only", "audit_events_no_truncate",
	"audit_heads_append_only", "audit_heads_no_truncate",
	"lineage_edges_acyclic", "runs_signed_immutable",
	"lineage_edges_sealed", "lineage_nodes_sealed", "instrument_readings_sealed",
	"protocol_versions_append_only", "protocol_versions_no_truncate",
	"protocol_versions_gapless", "runs_protocol_pinned",
}
```

## 3. UI

None. This is a Phase 0 engine-only spec.

## 4. Testing approach

The tests call the pure functions on fixtures in `tools/validate/testdata/`, which Go ignores when building. They never start a nested `go test`.

| Requirement | Test location |
|---|---|
| SPEC-004-R02 | `tools/validate/validate_test.go`, `TestVerdicts` |
| SPEC-004-R03 | `tools/validate/validate_test.go`, `TestSkippedOrMissingIsNotPassed` |

R04 (unknown references) is marked `Test: no`. Its fixture is cheap, but the testing policy keeps it untested unless the spec says otherwise.

## 5. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Requirement IDs are read from test comments with `go/parser` | Matches the existing `// SPEC-001-R03` convention; no test renames needed | Encoding IDs in test names |
| 2026-10-18 | The report is hashed, not signed, in v1 | No key management exists yet | Ad-hoc signing key now |
| 2026-10-22 | A requirement is VERIFIED only if every citing test passed | One passing test must not hide a failing one | Any one passing test is enough |
| 2026-10-22 | A dirty tree or any `go test` failure still writes the report but fails the run, with a banner | Evidence of what happened is useful; filing it is not | Refusing to run on a dirty tree |
| 2026-10-22 | The tool uses `pgx` for two read queries | Already a dependency; R06 needs the server version and applied migrations | Shelling out to `psql` (not always installed) |
| 2026-10-22 | The OQ report records the git commit, not a binary SHA | `make validate` tests the source tree; the binary SHA belongs to IQ (`labrat iq`) | Building a binary just to hash it |
| 2026-10-22 | The scanner skips `.`/`_`-prefixed dirs, `testdata` and `vendor` | Same set as `go test ./...`; `.claude/worktrees/` would otherwise double-count tests | Scanning everything |
| 2026-10-22 | `iqTriggers` is an explicit list | IQ must catch a *missing* trigger, which no catalog query can infer | Checking only that existing triggers are enabled |
| 2026-10-26 | Renamed from "Validation package v1" and added R12's scope line | The output is traceability evidence; the old name and README wording implied it could be filed | Keeping the name and relying on readers to know |
| 2026-10-22 | Compiled OQ binaries are deferred | Format should settle first (open question 1) | Shipping in v1 |
