# SPEC-004 design: Validation package v1

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: a stdlib-only Go program, `tools/validate`. It parses the requirement tables in every `specs/**/requirements.md` (including `specs/implemented/`), scans `*_test.go` with `go/parser` for `SPEC-NNN-Rnn` comments inside or above each test function, runs `go test -json -count=1 ./...`, joins the three, and writes `report.md`, `report.json` and `report.sha256`. `labrat iq` is a subcommand of the server binary, built on SQL queries against the catalog.
- Not doing: a test-management database, PDF generation, or a web dashboard.

## 2. Data and interfaces

- `make validate OPERATOR="J. Doe"` → `validation/<UTC timestamp>/{report.md, report.json, report.sha256}`. `validation/` is git-ignored.
- `report.json` shape: `{environment:{…}, requirements:[{id, spec, spec_status, tests:[{package, name, result, elapsed_s}], verdict}], unknown_references:[…], summary:{verified, not_verified, total}}`.
- `labrat iq [--json]`: uses `LDE_DATABASE_URL` and makes read-only catalog queries.

## 3. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Requirement IDs are read from test comments with `go/parser` | Matches the existing `// SPEC-001-R03` convention; no test renames needed | Encoding IDs in test names |
| 2026-10-18 | The report is hashed, not signed, in v1 | No key management exists yet | Ad-hoc signing key now |
