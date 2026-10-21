# SPEC-003 tasks: Immutable protocol versions

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

**Before Phase 1:** migration `0005` refuses to run on a database that has runs, and the SPEC-002 integration tests leave runs behind. Reset the dev/test database once before running the new migration: `docker compose down -v && docker compose up -d`.

## Phase 1: Database

- [ ] 1.1 Test: protocol version rules in the database (SPEC-003-R01, R02, R03, R04, R11)
  - Requirements: SPEC-003-R01, SPEC-003-R02, SPEC-003-R03, SPEC-003-R04, SPEC-003-R11
  - Files: `internal/storage/integration_test.go`
  - Do: add a helper `newProtocolVersion(t, tx, tenant, expID string, version int, md string) (id, nodeID string)`. It inserts a `protocol` lineage node with the existing `insertNode` constant, then `INSERT INTO protocol_versions (tenant_id, experiment_id, version, protocol_md, node_id, created_by) VALUES (…, 'it@lab') RETURNING id`. Then add one test function per rule, each with a `// SPEC-003-Rnn` comment, built from the acceptance examples in `requirements.md` §4:
    - `TestProtocolVersionHash` (R01): `"# A\n"` gives `sha256` = `sha256.Sum256([]byte("# A\n"))`. An INSERT that lists the `sha256` column returns an error.
    - `TestProtocolVersionsAreAppendOnly` (R02): `UPDATE`, `DELETE` and `TRUNCATE protocol_versions` each fail with `LDE03`, both inside `db.WithTenant` (as `lde_app`) and on `db.Pool` (as the owner). Run `TRUNCATE` only on `db.Pool`, since `lde_app` has no TRUNCATE grant. Use `wantCode`.
    - `TestProtocolVersionsAreGapless` (R03): with v1 and v2 on `E`, v4 fails and v3 succeeds. v1 on a new experiment `F` succeeds.
    - `TestRunProtocolMustBelongToExperiment` (R04): a run on `E` with `PF1` fails with SQLSTATE `23503`.
    - `TestRunProtocolPinIsImmutable` (R11): with a `planned` run on `E` pinned to `PE1`, `UPDATE runs SET protocol_version_id = PE2` fails with `LDE03` as `lde_app` and as the owner. `UPDATE runs SET state = 'running'` succeeds.
  - Verify: `go vet ./...` is clean. `make test-integration` fails only on these five tests (the table doesn't exist yet).
  - Integrity: yes
- [ ] 1.2 Migration `0005_protocol_versions.sql`, plus the SPEC-002 fixture
  - Requirements: SPEC-003-R01, R02, R03, R04, R09, R11, R12
  - Files: `internal/storage/migrations/0005_protocol_versions.sql` (new), `internal/storage/integration_test.go` (fixture only)
  - Do: write the migration with the ten steps in `design.md` §2, in that order, using the exact names given there (`protocol_sha256`, `protocol_versions_reject_mutation`, `protocol_versions_append_only`, `protocol_versions_no_truncate`, `protocol_versions_gapless`, `runs_reject_repin`, `runs_protocol_pinned`, `protocol_versions_meta_gin`). Add a header comment in the style of `0004_sealing.sql` and a `-- SPEC-003-Rnn` comment above each object. In `newSealFixture`, after inserting the experiment, call `newProtocolVersion(t, tx, f.tenant, expID, 1, "# p\n")`. Pass the returned id as `protocol_version_id` in the runs INSERT. Change nothing else in existing tests.
  - Verify: after the reset above, `make test-integration` passes in full (SPEC-001, SPEC-002 and the 1.1 tests). Then check the backfill by hand: `git worktree add ../lr-0004 dev`, and in that checkout run `docker compose down -v && docker compose up -d && go run ./cmd/server`, POST one experiment (README example), then stop it. Back on this branch, run `go run ./cmd/server` and confirm that `GET /v1/audit/verify` reports the chain valid and `GET /v1/experiments/{id}` returns `protocol_version: 1`. That GET only works after 2.2, so finish this check then. Also confirm that starting a DB that has a run stops with the reset message.
  - Integrity: yes

## Phase 2: API

- [ ] 2.1 Protocol version endpoints
  - Requirements: SPEC-003-R08, SPEC-003-R12, SPEC-003-R13
  - Files: `internal/api/protocols.go` (new), `internal/api/server.go` (routes only)
  - Do:
    - Type `protocolVersion` with JSON fields `id, experiment_id, version, sha256, node_id, created_by, created_at`, plus `protocol_md` and `protocol_meta` (`omitempty`) for the single read.
    - `addProtocolVersion(ctx, tx, p, expID, slug, md string) (protocolVersion, error)`, exactly as in `design.md` §2:
      - Return `badRequest` if `md == ""`. Parse with `frontmatter.Parse` and marshal `doc.Meta` the same way `createExperiment` does today.
      - Read the highest version's `encode(sha256,'hex')`. If it equals `hex(sha256.Sum256([]byte(md)))`, return `&httpError{http.StatusConflict, "protocol text is identical to version N"}`.
      - Compute the next version number. Then `insertNode(ctx, tx, p, domain.KindProtocol, fmt.Sprintf("%s v%d", slug, next), nil)`, and `INSERT … RETURNING` with `encode(sha256,'hex')`.
      - `audit.Append` the `protocol.version_created` event with the payload keys from `design.md` §2.
    - Handlers:
      - `createProtocolVersion` (`POST /v1/experiments/{id}/protocol-versions`, body `{protocol_md}`): look up the experiment's `slug`. If there's no row, return `notFound("experiment")`. Otherwise call the helper and answer `201`.
      - `listProtocolVersions` (`GET /v1/experiments/{id}/protocol-versions`): ordered by `version`, without `protocol_md` / `protocol_meta`.
      - `getProtocolVersion` (`GET /v1/protocol-versions/{id}`): the full row.
    - Register the three routes in `Handler()` after the experiment routes.
  - Verify: `gofmt -l . && go vet ./...` clean; `make test-integration` passes. Manually: POST v2 → `201` `version: 2`; POST the same text again → `409`; POST `""` → `400`.
- [ ] 2.2 Experiments read and write versions
  - Requirements: SPEC-003-R08, SPEC-003-R10, SPEC-003-R12
  - Files: `internal/api/experiments.go`
  - Do:
    - `createExperiment`: reject an empty `protocol_md` with `400` before the transaction. Remove `protocol_md` / `protocol_meta` from the `experiments` INSERT. Drop `protocol_sha256` from the `experiment.created` payload (keep `slug` and `title`). After that event, call `addProtocolVersion(…, exp.ID, exp.Slug, req.ProtocolMD)` in the same transaction.
    - Add `ProtocolVersionID string` (`protocol_version_id`), `ProtocolVersion int` (`protocol_version`) and `ProtocolSHA256 string` (`protocol_sha256`) to `experiment`, and fill them from the new version.
    - `getExperiment`: select the same fields with a `JOIN LATERAL (SELECT … FROM protocol_versions v WHERE v.tenant_id = e.tenant_id AND v.experiment_id = e.id ORDER BY version DESC LIMIT 1)`.
    - Remove the now-unused `crypto/sha256` and `encoding/hex` imports if nothing else uses them.
  - Verify: `gofmt -l . && go vet ./...` clean; `make test-integration` passes. The README `curl` example still creates an experiment, and its GET shows `protocol_version: 1`. Finish the 1.2 backfill check now.
- [ ] 2.3 Test: run creation pins, links and audits its protocol (SPEC-003-R06, R07)
  - Requirements: SPEC-003-R06, SPEC-003-R07
  - Files: `internal/api/integration_test.go` (new, `package api_test`)
  - Do: this is the first API-level test. Skip it unless `LDE_TEST_DATABASE_URL` is set. Open and migrate the DB like `openTestDB` in `internal/storage/integration_test.go` (copy the ~15 lines; don't export a shared helper). Create a tenant with `db.EnsureTenant`. Serve `api.New(config.Config{Env: "dev"}, db, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()` with `httptest.NewServer`. Send `X-LDE-Tenant` / `X-LDE-Actor` headers. One test, `TestRunPinsProtocolVersion`, with comment `// SPEC-003-R06, SPEC-003-R07`:
    - Create experiment `E` and read its `protocol_version_id` (`PE1`) and `protocol_sha256`.
    - `POST /v1/runs` with `{experiment_id: E}`. Check that the response's `protocol_version_id` is `PE1`.
    - Fetch `GET /v1/protocol-versions/PE1` for its `node_id`. Then `GET /v1/lineage/{run node_id}` (upstream) must contain that node, and an `edges` entry from it to the run node with relation `used`. Check the response shape in `lineage.go`.
    - Read the run's `run.created` event with `db.WithTenant` and `SELECT payload_json FROM audit_events WHERE action = 'run.created' AND entity_id = $1`. Check `protocol_version_id` = `PE1`, `protocol_version` = 1 and `protocol_sha256` = the experiment's `protocol_sha256`.
  - Verify: `make test-integration` fails only on this test.
  - Integrity: yes
- [ ] 2.4 Runs pin a protocol version
  - Requirements: SPEC-003-R04, SPEC-003-R05, SPEC-003-R06, SPEC-003-R07
  - Files: `internal/domain/run.go`, `internal/api/runs.go`
  - Do:
    - Add `ProtocolVersionID string` with `json:"protocol_version_id"` to `domain.Run`, after `ExperimentID`. Add `protocol_version_id` to `runColumns` and `scanRun` in the same position.
    - In `createRun`, accept an optional `protocol_version_id` and validate it with `validUUID` when present. Inside the transaction, read `id, version, encode(sha256,'hex'), node_id` from `protocol_versions`, either for the given id (`AND experiment_id = $exp`) or for the experiment's highest version. On `pgx.ErrNoRows`, return `notFound("protocol version")`.
    - Insert the run with `protocol_version_id`. Add the edge `{From: pv node, To: run node, Relation: domain.RelUsed}` with `insertEdge` before the input edges.
    - Add `protocol_version_id`, `protocol_version` and `protocol_sha256` to the `run.created` payload.
  - Verify: `gofmt -l . && go vet ./...` clean; `make test` and `make test-integration` pass, including 2.3 unchanged.
  - Integrity: yes

## Phase 3: Verify and close out

- [ ] 3.1 `verifier` passes the whole spec
- [ ] 3.2 `uat-planner` writes [uat.md](uat.md)
- [ ] 3.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated (the "Protocols as code" row and integrity gap 3)
- [ ] 3.4 Summary commit written as the PR description
