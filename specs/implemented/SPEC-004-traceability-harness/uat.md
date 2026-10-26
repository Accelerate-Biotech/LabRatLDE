# SPEC-004 user acceptance test plan: Traceability harness

| | |
|---|---|
| **Build** | commit `0d44cd7` on `worktree-spec-004-validation` |
| **Written** | 2026-10-23 |
| **Testers** | QA reviewer (validation lead), lab IT / installer, scientist or lab manager as the approver of the report |

## 1. Setup

Run from a clean checkout of the commit under test. Commands are bash-style (Git Bash, WSL or Linux/macOS). All data is synthetic. The operator name `J. Doe` is a placeholder.

```bash
git status --porcelain          # must print nothing (clean tree)
docker compose up -d            # Postgres 16 (user lde, password lde, database lde)
export TEST_DB="postgres://lde:lde@localhost:5432/lde?sslmode=disable"
```

Wait until `docker compose ps` shows `postgres` as healthy. Cases 1 to 3 need a clean tree and cases 4 to 6 deliberately dirty it, so run the cases in order and run the "Clean up" step after each case that says so.

Clean up (after cases 4, 5, 6):

```bash
git checkout -- . && git clean -fd -- tools internal cmd   # removes only the temporary edits made in the case
```

Cases 7 and 8 use the database that case 1 has already migrated. IQ never migrates, so case 1 must have run first.

## 2. Test cases

### UAT-01: Run the suite and get a traceability report

- **Requirements:** SPEC-004-R01, SPEC-004-R06, SPEC-004-R10 (clean-tree path)
- **As:** QA reviewer
- **Steps:**
  1. With a clean tree and the database up, run `make validate OPERATOR="J. Doe"`.
  2. Note the folder printed at the end (`validation/<UTC timestamp>/`), the summary line and the result line.
  3. Check the exit code: `echo $?`.
  4. Open `report.md` in that folder.
- **Expected:**
  - Exit code is 0 and the result is `PASS`. The folder contains `report.json`, `report.sha256` and `report.md`.
  - `report.md` starts with `# LabRat traceability report`, then the line saying it is test-traceability evidence only and not by itself sufficient for a regulatory filing (R12), then `**Result: PASS**`. There is no `NOT FOR FILING` or `go test reported failures` banner.
  - The Environment table shows: operator `J. Doe`, git commit (matches `git rev-parse HEAD`) and dirty flag false, Go version, OS/arch, PostgreSQL server version (16.x), the full list of applied migrations, and start and end times in UTC.
  - The Traceability matrix lists every `Test: yes` requirement of the Approved or Implemented specs (for example SPEC-001-R02, SPEC-004-R02, SPEC-004-R03), each with its citing tests marked `pass` and the verdict `VERIFIED`.
  - A requirement from a Draft spec (if any exists under `specs/`) is not listed. A `Test: no` requirement is not listed.
- **Result:** Pass / Fail · Notes:

### UAT-02: Check the report has not been altered

- **Requirements:** SPEC-004-R07
- **As:** QA reviewer
- **Steps:**
  1. In the folder from UAT-01, run `sha256sum -c report.sha256`.
  2. Compare the hash in `report.sha256` with the `report.json SHA-256:` line near the top of `report.md`.
  3. Change one character in a copy: `cp report.json /tmp/r.json`, edit it, then compare `sha256sum /tmp/r.json` with `report.sha256`.
- **Expected:** Step 1 prints `report.json: OK`. The two hashes in step 2 are identical. The edited copy's hash differs, showing tampering is detectable.
- **Result:** Pass / Fail · Notes:

### UAT-03: The report has a sign-off block for our own approvals

- **Requirements:** SPEC-004-R11
- **As:** QA reviewer, lab manager
- **Steps:**
  1. Scroll to the end of `report.md` from UAT-01.
- **Expected:** The last section is `## Sign-off`, a table with columns Role, Name, Signature, Date and the rows `Executed by`, `Reviewed by`, `QA approved`. All cells under Name, Signature and Date are blank, ready to be filled by hand or printed.
- **Result:** Pass / Fail · Notes:

### UAT-04: Refuse to run without a test database

- **Requirements:** SPEC-004-R05
- **As:** QA reviewer
- **Steps:**
  1. Run `LDE_TEST_DATABASE_URL= go run ./tools/validate --operator "J. Doe"`.
  2. Check `echo $?`, and `ls validation/` to see whether a new folder appeared.
- **Expected:** The tool prints `validate: LDE_TEST_DATABASE_URL is not set`, exits non-zero (2), no tests run, and no new folder is created under `validation/`.
- **Result:** Pass / Fail · Notes:

### UAT-05: A dirty working tree is stamped NOT FOR FILING

- **Requirements:** SPEC-004-R10, SPEC-004-R06
- **As:** QA reviewer
- **Steps:**
  1. Make an uncommitted change: `echo "# scratch" >> README.md`.
  2. Run `make validate OPERATOR="J. Doe"`. Check `echo $?`.
  3. Open the new `report.md`.
  4. Clean up: `git checkout -- README.md`.
- **Expected:** The run still completes and writes the three files, but the exit code is non-zero and the result is `FAIL`. The top of `report.md` (under the title) states `**NOT FOR FILING: uncommitted changes**`. The Environment table shows git dirty `true`.
- **Result:** Pass / Fail · Notes:

### UAT-06: A skipped or failing test makes a requirement NOT VERIFIED

- **Requirements:** SPEC-004-R02, SPEC-004-R03, SPEC-004-R10 (go test failure)
- **As:** QA reviewer
- **Steps:**
  1. Create a temporary test that cites a real requirement and skips:
     ```bash
     cat > internal/audit/uat_tmp_test.go <<'EOF'
     package audit

     import "testing"

     // SPEC-001-R02
     func TestUATSkipped(t *testing.T) { t.Skip("uat") }
     EOF
     ```
  2. Run `make validate OPERATOR="J. Doe"`. Check `echo $?`.
  3. In the new `report.md`, find SPEC-001-R02 in the matrix.
  4. Replace the skip with a failure: change the test body to `t.Fatal("uat")`, rerun and look at the top of `report.md` and the matrix again.
  5. Clean up (see Setup).
- **Expected:**
  - Step 2: exit non-zero, result `FAIL`. SPEC-001-R02 shows the skipped test as `skip` and the verdict `NOT VERIFIED`, even though its other citing tests pass. The summary counts at least one not verified.
  - Step 4: SPEC-001-R02 shows `fail`, verdict `NOT VERIFIED`, and `report.md` carries the banner `**go test reported failures**`.
  - The report is written in both runs (it also carries the dirty-tree banner because the temporary file is uncommitted, which is expected here).
- **Result:** Pass / Fail · Notes:

### UAT-07: A test citing a requirement that does not exist is flagged

- **Requirements:** SPEC-004-R04
- **As:** QA reviewer
- **Steps:**
  1. Create a temporary passing test citing a made-up ID:
     ```bash
     cat > internal/audit/uat_tmp_test.go <<'EOF'
     package audit

     import "testing"

     // SPEC-099-R01
     func TestUATUnknown(t *testing.T) {}
     EOF
     ```
  2. Run `make validate OPERATOR="J. Doe"`. Check `echo $?`.
  3. In `report.md`, open `## Unknown references`.
  4. Clean up (see Setup).
- **Expected:** Exit non-zero, result `FAIL`. The Unknown references table lists `SPEC-099-R01` with the package `internal/audit`, the test `TestUATUnknown` and its file. After clean-up and a clean rerun, the section reads `None.`
- **Result:** Pass / Fail · Notes:

### UAT-08: Installation qualification on a good install

- **Requirements:** SPEC-004-R08
- **As:** Lab IT / installer
- **Steps:**
  1. Confirm the database was migrated by UAT-01.
  2. Run `go run ./cmd/server iq` (the `labrat iq` subcommand; for a built binary, run `./<binary> iq`).
  3. Check `echo $?`.
  4. Build the binary with `go build -o /tmp/labrat ./cmd/server`, run `/tmp/labrat iq`, and compare the printed sha256 with `sha256sum /tmp/labrat`.
- **Expected:** Five lines starting with `PASS`, one each for `binary`, `migrations`, `rls`, `triggers` and `lde_app`. The `binary` line prints the version and a SHA-256, and in step 4 it equals the `sha256sum` result. `migrations` says all embedded migrations are applied, `rls` reports all tenant-owned tables enabled and forced, `triggers` reports 14 integrity triggers exist and are enabled, and `lde_app` says `NOSUPERUSER NOBYPASSRLS`. Exit code 0. No server starts.
- **Result:** Pass / Fail · Notes:

### UAT-09: Installation qualification catches a weakened database

- **Requirements:** SPEC-004-R09, SPEC-004-R08
- **As:** Lab IT / installer
- **Steps:**
  1. Disable an integrity trigger and an RLS setting:
     ```bash
     docker compose exec postgres psql -U lde -d lde -c "ALTER TABLE lineage_edges DISABLE TRIGGER lineage_edges_acyclic;"
     docker compose exec postgres psql -U lde -d lde -c "ALTER TABLE runs NO FORCE ROW LEVEL SECURITY;"
     ```
  2. Run `go run ./cmd/server iq`. Check `echo $?`.
  3. Delete a migration record to simulate an unapplied migration, then rerun:
     ```bash
     docker compose exec postgres psql -U lde -d lde -c "DELETE FROM schema_migrations WHERE version = (SELECT max(version) FROM schema_migrations);"
     go run ./cmd/server iq
     ```
  4. Restore everything: run `make validate OPERATOR="J. Doe"` once more. This re-migrates the database. Then re-enable the changes:
     ```bash
     docker compose exec postgres psql -U lde -d lde -c "ALTER TABLE lineage_edges ENABLE TRIGGER lineage_edges_acyclic;"
     docker compose exec postgres psql -U lde -d lde -c "ALTER TABLE runs FORCE ROW LEVEL SECURITY;"
     go run ./cmd/server iq
     ```
- **Expected:**
  - Step 2: a `FAIL  triggers` line naming `lineage_edges_acyclic on lineage_edges is disabled`, and a `FAIL  rls` line naming `runs: row level security is not forced`. The passing checks are still printed. The final error is `iq: 2 check(s) failed` and the exit code is non-zero.
  - Step 3: an additional `FAIL  migrations` line naming the missing embedded migration, with the exit code non-zero.
  - Step 4: the last IQ run is all `PASS` with exit code 0, confirming the database is back to its good state.
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-004-R01 | UAT-01 | — |
| SPEC-004-R02 | UAT-06 | yes |
| SPEC-004-R03 | UAT-06 | yes |
| SPEC-004-R04 | UAT-07 | — |
| SPEC-004-R05 | UAT-04 | — |
| SPEC-004-R06 | UAT-01, UAT-05 | — |
| SPEC-004-R07 | UAT-02 | — |
| SPEC-004-R08 | UAT-08, UAT-09 | — |
| SPEC-004-R09 | UAT-09 | — |
| SPEC-004-R10 | UAT-05, UAT-06 | — |
| SPEC-004-R11 | UAT-03 | — |

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
