# SPEC-003 user acceptance test plan: Immutable protocol versions

| | |
|---|---|
| **Build** | commit `fab6806` on `worktree-spec-003-review` |
| **Written** | 2026-10-21 |
| **Testers** | Scientist (runs the bench cases), QA reviewer (integrity and audit cases) |

The web UI arrives with SPEC-005, so every case here uses the public API with `curl`. Read-only SQL is used where QA needs to look inside the database. All data is synthetic.

## 1. Setup

You need Docker, Go, `curl`, `jq` and Python 3. Commands are for a POSIX shell (Git Bash or WSL on Windows).

1. **Start from a clean database.** Migration 0005 refuses to run on a database that already contains runs (see UAT-10). If you have used this checkout before, reset it first. This deletes all local dev data:

   ```bash
   docker compose down -v && docker compose up -d
   ```

2. **Port 5432 must be free.** If another Postgres on your machine already uses port 5432, the compose database is not reachable (the server will connect to the wrong database or fail to log in). Either stop the other Postgres, or change the host port mapping in `docker-compose.yml` (for example `"5433:5432"`) and start the server with `LDE_DATABASE_URL`:

   ```bash
   export LDE_DATABASE_URL='postgres://lde:lde@localhost:5433/lde?sslmode=disable'
   ```

3. **Start the server** (applies migrations, listens on :8080) in its own terminal and leave it running:

   ```bash
   go run ./cmd/server
   ```

4. **Set up the shell** used by every case:

   ```bash
   export API=http://localhost:8080
   export T=00000000-0000-4000-8000-000000000001
   H=(-H "X-LDE-Tenant: $T" -H "X-LDE-Actor: scientist.a@example.test" -H "Content-Type: application/json")
   psql_lde() { docker compose exec -T postgres psql -U lde -d lde "$@"; }
   curl -s $API/healthz        # expect {"status":"ok"}
   ```

   The dev tenant `00000000-0000-4000-8000-000000000001` is created by the migrations. If a tenant-related error appears, check the server log.

5. Test protocol texts used below (synthetic):

   ```bash
   P1=$'---\nname: elisa-binding\nparams:\n  incubation_min: 60\n---\n# ELISA binding\n1. Coat plate.\n'
   P2=$'---\nname: elisa-binding\nparams:\n  incubation_min: 90\n---\n# ELISA binding\n1. Coat plate.\n2. Extend incubation to 90 min.\n'
   ```

## 2. Test cases

### UAT-01: Create an experiment and get protocol version 1

- **Requirements:** SPEC-003-R08, SPEC-003-R10
- **As:** Scientist
- **Steps:**
  1. Create the experiment:
     ```bash
     EXP=$(curl -s "${H[@]}" -X POST $API/v1/experiments \
       -d "$(jq -n --arg md "$P1" '{slug:"elisa-001",title:"ELISA binding (UAT)",protocol_md:$md}')")
     echo "$EXP" | jq .
     EID=$(echo "$EXP" | jq -r .id); PV1=$(echo "$EXP" | jq -r .protocol_version_id)
     ```
  2. Read it back: `curl -s "${H[@]}" $API/v1/experiments/$EID | jq .`
  3. List its versions: `curl -s "${H[@]}" $API/v1/experiments/$EID/protocol-versions | jq .`
- **Expected:**
  - Step 1 returns HTTP 201 with `protocol_version` = `1`, a `protocol_version_id`, a 64-character lowercase hex `protocol_sha256`, and `protocol_md` equal to the text sent.
  - Step 2 returns 200 with the same version number, text and `protocol_meta` (frontmatter as JSON, `name` = `elisa-binding`).
  - Step 3 returns exactly one entry: `version` 1, with `sha256` and `node_id`, and no `protocol_md` field (the list omits the text).
  - (Audit event checked in UAT-09.)
- **Result:** Pass / Fail · Notes:

### UAT-02: Add a revised protocol as version 2

- **Requirements:** SPEC-003-R08, SPEC-003-R10 (and the version API in scope)
- **As:** Scientist
- **Steps:**
  1. Add version 2:
     ```bash
     V2=$(curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/experiments/$EID/protocol-versions \
       -d "$(jq -n --arg md "$P2" '{protocol_md:$md}')")
     echo "$V2"
     PV2=$(echo "$V2" | head -1 | jq -r .id)
     ```
  2. List versions again, then read the experiment, then read version 1 by id:
     ```bash
     curl -s "${H[@]}" $API/v1/experiments/$EID/protocol-versions | jq '[.[]|{version,sha256}]'
     curl -s "${H[@]}" $API/v1/experiments/$EID | jq '{protocol_version,protocol_md}'
     curl -s "${H[@]}" $API/v1/protocol-versions/$PV1 | jq '{version,protocol_md}'
     ```
- **Expected:**
  - Step 1 returns HTTP 201 with `version` = `2`, a new `id`/`node_id`, and a `sha256` different from version 1.
  - The list shows versions 1 and 2 in order, with no gaps.
  - The experiment now reports `protocol_version` = `2` and the version 2 text.
  - Version 1 is still readable and still has the original (first) text.
- **Result:** Pass / Fail · Notes:

### UAT-03: Refuse an unchanged or empty protocol

- **Requirements:** SPEC-003-R13, SPEC-003-R12
- **As:** Scientist
- **Steps:**
  1. Resubmit the exact text of the latest version (version 2):
     ```bash
     curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/experiments/$EID/protocol-versions \
       -d "$(jq -n --arg md "$P2" '{protocol_md:$md}')"
     ```
  2. Submit an empty protocol:
     ```bash
     curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/experiments/$EID/protocol-versions -d '{"protocol_md":""}'
     ```
  3. Try to create an experiment with an empty protocol:
     ```bash
     curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/experiments -d '{"slug":"empty-001","title":"Empty","protocol_md":""}'
     ```
  4. List versions again.
- **Expected:**
  - Step 1: HTTP `409`, error says the text is identical to version 2.
  - Steps 2 and 3: HTTP `400`, error says `protocol_md is required`.
  - Step 4: still exactly versions 1 and 2. Nothing was created. Re-adding the text of version 1 (`$P1`) is allowed because only the latest version is compared; do not do this now, as it would add a version 3 and change later steps.
- **Result:** Pass / Fail · Notes:

### UAT-04: A run follows the latest protocol by default

- **Requirements:** SPEC-003-R05
- **As:** Scientist
- **Steps:**
  1. Start a run without naming a version:
     ```bash
     RUN=$(curl -s "${H[@]}" -X POST $API/v1/runs \
       -d "{\"experiment_id\":\"$EID\",\"label\":\"ELISA run 1\",\"params\":{\"plate\":\"P-001\"}}")
     echo "$RUN" | jq .
     RID=$(echo "$RUN" | jq -r .id); RNODE=$(echo "$RUN" | jq -r .node_id)
     echo "pinned: $(echo "$RUN" | jq -r .protocol_version_id)   latest: $PV2"
     ```
  2. Read it back: `curl -s "${H[@]}" $API/v1/runs/$RID | jq '{state,protocol_version_id}'`
- **Expected:** Step 1 returns HTTP 201, state `planned`, and `protocol_version_id` equal to `$PV2` (version 2, the latest). Step 2 shows the same pin.
- **Result:** Pass / Fail · Notes:

### UAT-05: A run can follow a named earlier version

- **Requirements:** SPEC-003-R05 (named-version path), SPEC-003-R04
- **As:** Scientist
- **Steps:**
  1. Start a run pinned to version 1:
     ```bash
     RUN1=$(curl -s "${H[@]}" -X POST $API/v1/runs \
       -d "{\"experiment_id\":\"$EID\",\"protocol_version_id\":\"$PV1\",\"label\":\"ELISA run 2 (v1)\"}")
     echo "$RUN1" | jq '{id,state,protocol_version_id}'
     RID1=$(echo "$RUN1" | jq -r .id); RNODE1=$(echo "$RUN1" | jq -r .node_id)
     ```
  2. Negative: create a second experiment and try to use its protocol version on the first experiment's run:
     ```bash
     F=$(curl -s "${H[@]}" -X POST $API/v1/experiments \
       -d "$(jq -n --arg md "$P1" '{slug:"western-001",title:"Western (UAT)",protocol_md:$md}')")
     FID=$(echo "$F" | jq -r .id); PVF=$(echo "$F" | jq -r .protocol_version_id)
     curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/runs \
       -d "{\"experiment_id\":\"$EID\",\"protocol_version_id\":\"$PVF\"}"
     ```
- **Expected:**
  - Step 1: HTTP 201 and `protocol_version_id` = `$PV1`, even though version 2 exists.
  - Step 2: the request is refused (HTTP `404`, "protocol version not found", because the version does not belong to that experiment) and no run is created. The database also rejects the combination (foreign key), checked in UAT-08.
- **Result:** Pass / Fail · Notes:

### UAT-06: See which protocol text a run followed

- **Requirements:** SPEC-003-R06, SPEC-003-R10
- **As:** Scientist
- **Steps:**
  1. Open the run's lineage: `curl -s "${H[@]}" "$API/v1/lineage/$RNODE?direction=upstream" | jq '{nodes:[.nodes[]|{id,kind,label}],edges}'`
  2. Do the same for the version 1 run: `curl -s "${H[@]}" "$API/v1/lineage/$RNODE1?direction=upstream" | jq '{nodes:[.nodes[]|{id,kind,label}],edges}'`
  3. Look at the downstream view from the version 2 protocol node. First get its node id: `PN2=$(curl -s "${H[@]}" $API/v1/protocol-versions/$PV2 | jq -r .node_id)` then `curl -s "${H[@]}" "$API/v1/lineage/$PN2?direction=downstream" | jq '[.nodes[]|{kind,label}]'`
- **Expected:**
  - Step 1: nodes include a `protocol` node labelled `elisa-001 v2` and the `run` node `ELISA run 1`, with an edge from the protocol node to the run node, relation `used`.
  - Step 2: the protocol node is `elisa-001 v1`, joined to `ELISA run 2 (v1)` by a `used` edge (not v2).
  - Step 3: downstream of v2 contains the `ELISA run 1` run and not the version 1 run.
- **Result:** Pass / Fail · Notes:

### UAT-07: Version text and hash cannot be tampered with

- **Requirements:** SPEC-003-R01, SPEC-003-R02
- **As:** QA reviewer
- **Steps:**
  1. Check the stored hash matches an independent hash of the text:
     ```bash
     curl -s "${H[@]}" $API/v1/protocol-versions/$PV2 | jq -j .protocol_md | sha256sum
     curl -s "${H[@]}" $API/v1/protocol-versions/$PV2 | jq -r .sha256
     ```
  2. Try to supply the hash on insert (as the database owner, to bypass RLS setup):
     ```bash
     psql_lde -c "INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, sha256, node_id, created_by)
                  SELECT tenant_id, experiment_id, 99, 'x', '\x00', node_id, 'qa' FROM protocol_versions LIMIT 1;"
     ```
  3. As the owner (`lde`), try to change and remove versions:
     ```bash
     psql_lde -c "UPDATE protocol_versions SET protocol_md = 'tampered';"
     psql_lde -c "DELETE FROM protocol_versions;"
     psql_lde -c "TRUNCATE protocol_versions, runs;"
     ```
  4. As the application role `lde_app` (set the tenant first, as the API does):
     ```bash
     psql_lde -c "BEGIN; SET LOCAL ROLE lde_app; SELECT set_config('app.tenant_id','$T',true); UPDATE protocol_versions SET protocol_md = 'tampered'; ROLLBACK;"
     psql_lde -c "BEGIN; SET LOCAL ROLE lde_app; SELECT set_config('app.tenant_id','$T',true); DELETE FROM protocol_versions; ROLLBACK;"
     ```
  5. Re-read version 2 through the API and compare the text and `sha256` to step 1.
- **Expected:**
  - Step 1: the two hashes are identical (the `sha256sum` output has a trailing ` -`; ignore it).
  - Step 2: fails with an error that `sha256` is a generated column (SQLSTATE 428C9); no row is inserted.
  - Step 3: each statement fails with `protocol version ... is immutable` / `protocol versions are immutable (TRUNCATE rejected)` (SQLSTATE `LDE03`). A bare `TRUNCATE` may instead be stopped earlier by the `runs` foreign key (`0A000`); the one listed above should show `LDE03`.
  - Step 4: `UPDATE` and `DELETE` fail with `permission denied for table protocol_versions` (SQLSTATE 42501).
  - Step 5: unchanged text and hash.
- **Result:** Pass / Fail · Notes:

### UAT-08: Versions are gapless and a run's pin cannot change

- **Requirements:** SPEC-003-R03, SPEC-003-R04, SPEC-003-R11
- **As:** QA reviewer
- **Steps:**
  1. Try to insert a version that skips a number (experiment has versions 1 and 2, so 4 skips 3), as the owner:
     ```bash
     psql_lde -c "INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, node_id, created_by)
                  SELECT tenant_id, experiment_id, 4, 'skipped', node_id, 'qa' FROM protocol_versions WHERE id = '$PV1';"
     ```
  2. Try to re-point the version 2 run at version 1, as the owner and as `lde_app`:
     ```bash
     psql_lde -c "UPDATE runs SET protocol_version_id = '$PV1' WHERE id = '$RID';"
     psql_lde -c "BEGIN; SET LOCAL ROLE lde_app; SELECT set_config('app.tenant_id','$T',true); UPDATE runs SET protocol_version_id = '$PV1' WHERE id = '$RID'; ROLLBACK;"
     ```
  3. Try to pin a run to another experiment's version directly in the database:
     ```bash
     psql_lde -c "INSERT INTO runs (tenant_id, experiment_id, protocol_version_id, node_id, created_by)
                  SELECT tenant_id, experiment_id, '$PVF', node_id, 'qa' FROM runs WHERE id = '$RID';"
     ```
  4. Confirm normal run progress is unaffected: start the version 2 run through the API:
     ```bash
     curl -s -w '\n%{http_code}\n' "${H[@]}" -X POST $API/v1/runs/$RID/transitions -d '{"to":"running","reason":"UAT start"}'
     ```
- **Expected:**
  - Step 1: rejected with a check violation ("is not the next version"); no row is added.
  - Step 2: both attempts fail with `run ... protocol version is pinned` (SQLSTATE `LDE03`).
  - Step 3: rejected with a foreign key violation (the version belongs to a different experiment).
  - Step 4: HTTP 200, `state` = `running`, and `protocol_version_id` unchanged.
- **Result:** Pass / Fail · Notes:

### UAT-09: The audit trail records versions and run pins, and the chain verifies

- **Requirements:** SPEC-003-R07, SPEC-003-R08 (and the audit chain from SPEC-001)
- **As:** QA reviewer
- **Steps:**
  1. Find the protocol and run events:
     ```bash
     curl -s "${H[@]}" "$API/v1/audit/events?limit=1000" \
       | jq '.[] | select(.action=="protocol.version_created" or .action=="run.created") | {seq,actor,action,payload}'
     ```
  2. Check the event order for experiment creation:
     ```bash
     curl -s "${H[@]}" "$API/v1/audit/events?limit=1000" | jq -r '.[] | select(.action|test("^(experiment|protocol)\\.")) | "\(.seq) \(.action)"'
     ```
  3. Verify the chain in the server, then offline with the independent verifier:
     ```bash
     curl -s "${H[@]}" $API/v1/audit/verify | jq '{valid,agree}'
     curl -s "${H[@]}" $API/v1/audit/export.ndjson > /tmp/uat003-audit.ndjson
     python3 -I tools/verify_audit.py /tmp/uat003-audit.ndjson
     ```
- **Expected:**
  - Step 1: two `protocol.version_created` events for `elisa-001` (payload `version` 1 and 2, with `sha256` equal to the values from UAT-01/02, plus `experiment_id` and `node_id`) and one for `western-001`. Each `run.created` payload has `protocol_version_id`, `protocol_version` (2 for "ELISA run 1", 1 for "ELISA run 2 (v1)") and `protocol_sha256` equal to that version's `sha256`. The actor is `scientist.a@example.test`.
  - Step 2: for each experiment, `experiment.created` appears immediately before its `protocol.version_created` (possibly with `lineage.node_created` between them).
  - Step 3: `valid: true`, `agree: true`; the Python verifier prints `OK: <n> events, head seq ...`.
- **Result:** Pass / Fail · Notes:

### UAT-10: The migration backfills existing experiments and refuses databases with runs

- **Requirements:** SPEC-003-R09
- **As:** QA reviewer
- **Steps:**
  1. Stop the server. Reset the database: `docker compose down -v && docker compose up -d`.
  2. Build the previous schema: find the commit before migration 0005 and check it out into a scratch worktree:
     ```bash
     SHA=$(git log --format=%h -1 -- internal/storage/migrations/0005_protocol_versions.sql)
     git worktree add ../uat003-before "$SHA^"
     (cd ../uat003-before && go run ./cmd/server)    # leave running in another terminal
     ```
  3. Using the same headers, create two experiments on the old build (old API: protocol text is stored on the experiment):
     ```bash
     curl -s "${H[@]}" -X POST $API/v1/experiments -d "$(jq -n --arg md "$P1" '{slug:"old-a",title:"Old A",protocol_md:$md}')" | jq -r .id
     curl -s "${H[@]}" -X POST $API/v1/experiments -d "$(jq -n --arg md "$P2" '{slug:"old-b",title:"Old B",protocol_md:$md}')" | jq -r .id
     ```
     Stop the old server.
  4. Start the current build (`go run ./cmd/server` from this checkout). It migrates to 0005.
  5. List versions for both experiments (use the ids printed in step 3), then check the audit and the dropped columns:
     ```bash
     curl -s "${H[@]}" $API/v1/experiments/<id-of-old-a>/protocol-versions | jq .
     curl -s "${H[@]}" "$API/v1/audit/events?limit=1000" | jq '.[]|select(.action=="protocol.version_created")|{seq,actor,reason,payload}'
     curl -s "${H[@]}" $API/v1/audit/verify | jq '{valid,agree}'
     psql_lde -c "\d experiments" | grep protocol || echo "no protocol columns"
     ```
  6. **Reset guard.** Stop the current server. Reset again and bring the old build back to create a run:
     ```bash
     docker compose down -v && docker compose up -d
     (cd ../uat003-before && go run ./cmd/server)    # other terminal
     EXPID=$(curl -s "${H[@]}" -X POST $API/v1/experiments -d "$(jq -n --arg md "$P1" '{slug:"old-c",title:"Old C",protocol_md:$md}')" | jq -r .id)
     curl -s "${H[@]}" -X POST $API/v1/runs -d "{\"experiment_id\":\"$EXPID\"}" | jq .state
     ```
     Stop the old server, then run `go run ./cmd/server` from this checkout.
  7. Clean up: `docker compose down -v && docker compose up -d`, then `git worktree remove ../uat003-before`.
- **Expected:**
  - Step 5: each old experiment has exactly one version (`version` 1) whose text equals what was stored. The audit shows one `protocol.version_created` per experiment with actor `system:migration-0005`, reason `SPEC-003 backfill` and payload `version` 1 with the `sha256`. `valid: true` and `agree: true`, so the backfilled events are properly chained. `\d experiments` shows no `protocol_md` or `protocol_meta` column.
  - Step 6: the old build creates the run (`planned`). The current server then fails to start with an error containing `SPEC-003 migration needs a database with no runs and no empty protocols; reset it with: docker compose down -v`. The database is not partly migrated (`psql_lde -c "\dt protocol_versions"` reports no such table).
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-003-R01 | UAT-07 | yes |
| SPEC-003-R02 | UAT-07 | yes |
| SPEC-003-R03 | UAT-08 | yes |
| SPEC-003-R04 | UAT-05, UAT-08 | yes |
| SPEC-003-R05 | UAT-04, UAT-05 | no |
| SPEC-003-R06 | UAT-06 | yes |
| SPEC-003-R07 | UAT-09 | yes |
| SPEC-003-R08 | UAT-01, UAT-02, UAT-09 | no |
| SPEC-003-R09 | UAT-10 | no |
| SPEC-003-R10 | UAT-01, UAT-02, UAT-06 | no |
| SPEC-003-R11 | UAT-08 | yes |
| SPEC-003-R12 | UAT-03 | no |
| SPEC-003-R13 | UAT-03 | no |

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
