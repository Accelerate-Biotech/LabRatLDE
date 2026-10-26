# SPEC-004: Traceability harness

| | |
|---|---|
| **Status** | Implemented |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-26 |
| **Regulatory refs** | 21 CFR 11.10(a); EU Annex 11 §4 (validation) |
| **Depends on** | `SPEC-001` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Labs adopting LabRat have to validate it: show that it is installed correctly (IQ) and works as specified (OQ). Today that means writing Word documents by hand. LabRat already has the raw material for an executable OQ: specs with `Test: yes` requirements, and tests that cite those requirement IDs. What's missing is a command that runs them against a real database and produces a traceability matrix in a stamped, tamper-evident report, plus install checks.

This spec was first titled "Validation package v1". It was renamed on 2026-10-26, because what it delivers is test-traceability evidence, not a validation package. A filing also needs a risk assessment, pre-approved IQ/OQ protocols with expected and actual results, and a system that meets Part 11 (identity, roles and e-signatures, SPEC-006 to SPEC-008). That documentation is reserved as SPEC-020.

## 2. Scope

**In scope**
- `make validate`: runs the full test suite (including integration tests) against a database, maps tests to requirement IDs, and writes a report.
- A traceability matrix: requirement → spec status → test(s) → result.
- A report stamped with environment facts and protected by its own SHA-256.
- `labrat iq`: installation checks against a running database.

**Out of scope**
- Validation documentation: a risk assessment justifying the `Test: yes` selection, IQ/OQ protocol templates with pre-execution approval, tests that capture expected and actual results, and compiled OQ test binaries for customer sites (open question 1). Reserved as SPEC-020, after SPEC-008.
- Signing the report with a key. v1 records a hash only; SPEC-018's key could sign it later.
- Browser (Playwright) tests. SPEC-005 adds the harness; their results join the matrix in the spec that first adds a UI `Test: yes` requirement.
- PQ (performance qualification), which is customer-specific.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-004-R01 | When `make validate` runs, the validation tool shall list every requirement marked `Test: yes` in specs with status Approved or Implemented, along with the tests that cite it and each test's result. | no | 11.10(a) |
| SPEC-004-R02 | If a `Test: yes` requirement in an Approved or Implemented spec has no citing test, or any citing test did not pass, then the validation tool shall mark the requirement `NOT VERIFIED` and exit non-zero. | yes | 11.10(a) |
| SPEC-004-R03 | The validation tool shall count a skipped test, or a cited test with no result, as not passed. | yes | 11.10(a) |
| SPEC-004-R04 | If a test cites a requirement ID that doesn't exist in `specs/`, then the validation tool shall list it under "unknown references" and exit non-zero. | no | traceability |
| SPEC-004-R05 | If the test database URL (`LDE_TEST_DATABASE_URL`) is not set, then the validation tool shall stop before running tests and exit non-zero. | no | 11.10(a) |
| SPEC-004-R06 | The validation report shall record the git commit and dirty flag, Go version, OS/arch, PostgreSQL server version, applied migration list, the run's start and end time (UTC), and the operator name passed with `--operator`. | no | Annex 11 §4.4 |
| SPEC-004-R10 | If the git working tree has uncommitted changes, or `go test` reports any failure, then the validation tool shall still write the report, state the reason at the top of `report.md` (`NOT FOR FILING: uncommitted changes` for a dirty tree), and exit non-zero. | no | Annex 11 §4.4 |
| SPEC-004-R07 | The validation tool shall write `report.sha256` holding the SHA-256 of `report.json`, and `report.md` shall print the same hash. | no | tamper evidence |
| SPEC-004-R08 | When `labrat iq` runs, it shall check that all embedded migrations are applied, every tenant-owned table has RLS enabled and forced, the append-only and seal triggers exist and are enabled, `lde_app` is `NOSUPERUSER NOBYPASSRLS`, and it shall print the binary's own SHA-256 and version. | no | IQ |
| SPEC-004-R09 | If any `labrat iq` check fails, then `labrat iq` shall print each failure and exit non-zero. | no | IQ |
| SPEC-004-R11 | `report.md` shall end with a blank sign-off table (Executed by, Reviewed by, QA approved; each with name, signature and date) for the customer's own approval. | no | Annex 11 §4 |
| SPEC-004-R12 | `report.md` shall state, directly under its title, that it is test-traceability evidence only and is not by itself sufficient for a regulatory filing. | no | scope honesty |

## 4. Acceptance examples

The tool's own tests use fixture spec and test directories under `tools/validate/testdata/`, not the real `specs/`.

**SPEC-004-R02**
- Given an Approved spec with `SPEC-900-R01` (`Test: yes`) and `SPEC-900-R02` (`Test: yes`), and one passing test citing only R01.
- When the tool runs.
- Then R02 is `NOT VERIFIED`, R01 is `VERIFIED`, and the exit code is non-zero.
- And given a second, failing test that also cites R01, R01 is `NOT VERIFIED`.
- And given a Draft spec `SPEC-901` with an uncited `Test: yes` requirement, the exit code is unaffected.

**SPEC-004-R03**: given a test citing `SPEC-900-R01` that calls `t.Skip`, R01 is `NOT VERIFIED`. Given a test citing `SPEC-900-R01` with no result (its package didn't build), R01 is `NOT VERIFIED`.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should a release ship the integration tests as a compiled binary (`go test -c`) so a customer can run the OQ without Go or the source? Recommendation: yes, as a follow-up once the format settles. | Rob Bassett | Deferred to a follow-up spec; not in v1 (2026-10-22) |
| 2 | Does the report need a template section for the customer's protocol/approval signatures (pre-execution and post-execution)? | Rob Bassett | Yes: a blank sign-off table at the end of `report.md`, R11 (2026-10-22) |
