# SPEC-004: Validation package v1

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | 21 CFR 11.10(a); EU Annex 11 §4 (validation) |
| **Depends on** | `SPEC-001` |

## 1. Problem

Labs adopting LabRat have to validate it: show that it is installed correctly (IQ) and works as specified (OQ). Today that means writing Word documents by hand. LabRat already has the raw material for an executable OQ: specs with `Test: yes` requirements, and tests that cite those requirement IDs. What's missing is a command that runs them against a real database and produces evidence a QA reviewer can file: a traceability matrix and a stamped, tamper-evident report.

## 2. Scope

**In scope**
- `make validate`: runs the full test suite (including integration tests) against a database, maps tests to requirement IDs, and writes a report.
- A traceability matrix: requirement → spec status → test(s) → result.
- A report stamped with environment facts and protected by its own SHA-256.
- `labrat iq`: installation checks against a running database.

**Out of scope**
- Shipping compiled OQ test binaries to customer sites (open question 1).
- Signing the report with a key. v1 records a hash only; SPEC-018's key could sign it later.
- Browser (Playwright) tests. SPEC-005 adds the harness; their results join the matrix in the spec that first adds a UI `Test: yes` requirement.
- PQ (performance qualification), which is customer-specific.

## 3. Simplicity budget

- Approach: a stdlib-only Go program, `tools/validate`. It parses `specs/*.md` requirement tables, scans `*_test.go` with `go/parser` for `SPEC-NNN-Rnn` comments inside or above each test function, runs `go test -json -count=1 ./...`, joins the three, and writes `report.md`, `report.json` and `report.sha256`. `labrat iq` is a subcommand of the server binary, built on SQL queries against the catalog.
- Not doing: a test-management database, PDF generation, or a web dashboard.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-004-R01 | When `make validate` runs, the validation tool shall list every requirement marked `Test: yes` in specs with status Approved or Implemented, along with the tests that cite it and each test's result. | no | 11.10(a) |
| SPEC-004-R02 | If a `Test: yes` requirement in an Approved or Implemented spec has no cited test that passed, then the validation tool shall mark the requirement `NOT VERIFIED` and exit non-zero. | yes | 11.10(a) |
| SPEC-004-R03 | The validation tool shall count a skipped test as not passed. | yes | 11.10(a) |
| SPEC-004-R04 | If a test cites a requirement ID that doesn't exist in `specs/`, then the validation tool shall list it under "unknown references" and exit non-zero. | no | traceability |
| SPEC-004-R05 | If the test database URL (`LDE_TEST_DATABASE_URL`) is not set, then the validation tool shall stop before running tests and exit non-zero. | no | 11.10(a) |
| SPEC-004-R06 | The validation report shall record the git commit and dirty flag, Go version, OS/arch, PostgreSQL server version, applied migration list, the run's start and end time (UTC), and the operator name passed with `--operator`. | no | Annex 11 §4.4 |
| SPEC-004-R07 | The validation tool shall write `report.sha256` holding the SHA-256 of `report.json`, and `report.md` shall print the same hash. | no | tamper evidence |
| SPEC-004-R08 | When `labrat iq` runs, it shall check that all embedded migrations are applied, every tenant-owned table has RLS enabled and forced, the append-only and seal triggers exist and are enabled, `lde_app` is `NOSUPERUSER NOBYPASSRLS`, and it shall print the binary's own SHA-256 and version. | no | IQ |
| SPEC-004-R09 | If any `labrat iq` check fails, then `labrat iq` shall print each failure and exit non-zero. | no | IQ |

## 5. Acceptance examples

The tool's own tests use fixture spec and test directories under `tools/validate/testdata/`, not the real `specs/`.

**SPEC-004-R02**
- Given an Approved spec with `SPEC-900-R01` (`Test: yes`) and `SPEC-900-R02` (`Test: yes`), and one passing test citing only R01.
- When the tool runs.
- Then R02 is `NOT VERIFIED`, R01 is `VERIFIED`, and the exit code is non-zero.
- And given a Draft spec `SPEC-901` with an uncited `Test: yes` requirement, the exit code is unaffected.

**SPEC-004-R03**: given a test citing `SPEC-900-R01` that calls `t.Skip`, R01 is `NOT VERIFIED`.

## 6. Data and interfaces

- `make validate OPERATOR="J. Doe"` → `validation/<UTC timestamp>/{report.md, report.json, report.sha256}`. `validation/` is git-ignored.
- `report.json` shape: `{environment:{…}, requirements:[{id, spec, spec_status, tests:[{package, name, result, elapsed_s}], verdict}], unknown_references:[…], summary:{verified, not_verified, total}}`.
- `labrat iq [--json]`: uses `LDE_DATABASE_URL` and makes read-only catalog queries.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should a release ship the integration tests as a compiled binary (`go test -c`) so a customer can run the OQ without Go or the source? Recommendation: yes, as a follow-up once the format settles. | Rob Bassett | |
| 2 | Does the report need a template section for the customer's protocol/approval signatures (pre-execution and post-execution)? | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Requirement IDs are read from test comments with `go/parser` | Matches the existing `// SPEC-001-R03` convention; no test renames needed | Encoding IDs in test names |
| 2026-10-18 | The report is hashed, not signed, in v1 | No key management exists yet | Ad-hoc signing key now |
