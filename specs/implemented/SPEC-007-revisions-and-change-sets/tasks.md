# SPEC-007 tasks: Revisions, change sets and notes

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

`make test-integration` needs `docker compose up -d`. This spec's migrations put revision triggers on tables other branches write to, so give the spec a database of its own while it is in flight: run `CREATE DATABASE lde_spec007 OWNER lde`, then `make test-integration TEST_DB=postgres://lde:lde@localhost:5432/lde_spec007?sslmode=disable`.

Every task ends with `gofmt -l .` printing nothing and `go vet ./...` clean. UI tasks also end with `cd web && npm run build` clean.

Storage tests run SQL directly, as the owner (`db.Pool`) or as `lde_app` (`db.WithTenant`). Several tests share one helper, which task 1.1 adds to `internal/storage/integration_test.go`:

- `commitChangeSet(ctx context.Context, tx pgx.Tx, tenant, author, reason string) (string, error)` inserts a change set (title `edit`, the given `reason` and `author`, `state` `committed`, `decision_reason` = reason, `items` `[]`). It returns the id and then runs `SELECT set_config('app.change_set_id', $1, true)`. Run it as `lde_app`, so the guard stamps the author and decider.
- `proposeChangeSet(ctx, tx, tenant, author string) (string, error)` inserts a `proposed` change set (title `proposal`, reason `fixture`, items `[]`) and returns its id.

## Phase 1: Database

- [x] 1.1 Test: agents and services can't commit or reject (SPEC-007-R06)
  - Requirements: SPEC-007-R06
  - Files: `internal/storage/integration_test.go`
  - Do:
    - Add `commitChangeSet` and `proposeChangeSet` (above).
    - Add `TestAgentsCannotDecideChangeSets`. Use `newTenant` plus `newPrincipal` for `alice` (human; scientist), `bot` (agent; scientist, reviewer) and `svc` (service; scientist). In `WithTenant(…, as(tenant), …)`, call `delegate(ctx, tx, tenant, "it@lab", bot.ID, alice.ID, as(tenant).ID)` (it@lab is admin).
    - `botForAlice := storage.Principal{ID: bot.ID, OnBehalfOf: alice.ID}`.
    - Cases. Each runs in its own `WithTenant` and is checked with `wantCode`:
      1. botForAlice `proposeChangeSet(…, "alice via bot")` succeeds and gives `cs1`.
      2. botForAlice `UPDATE change_sets SET state = 'committed', decision_reason = 'ok' WHERE id = cs1` fails with `LDE05`.
      3. botForAlice `UPDATE … SET state = 'rejected', decision_reason = 'no' …` fails with `LDE05`.
      4. botForAlice `commitChangeSet(…)` (an insert straight into `committed`) fails with `LDE05`.
      5. svc `proposeChangeSet` succeeds and gives `cs2`. svc committing `cs2` fails with `LDE05`.
      6. alice committing `cs1` (`UPDATE … SET state = 'committed', decision_reason = 'ok'`) succeeds.
    - Comment `// SPEC-007-R06`.
  - Verify: `make test-integration TEST_DB=…` fails only on the new test (`change_sets` doesn't exist).
  - Integrity: yes
- [x] 1.2 Migration `0011_change_sets`
  - Requirements: SPEC-007-R06, SPEC-007-R12
  - Files: `internal/storage/migrations/0011_change_sets.sql`
  - Do:
    - `change_sets` exactly as the design §2 table, with its checks and index. `items_sha256` uses the existing `protocol_sha256(items::text)`. The state `CHECK` is `state IN ('proposed', 'committed', 'rejected', 'withdrawn')`. The source `CHECK` is `source IN ('human', 'parser', 'agent')`, default `'human'`.
    - `lde_require_human()` (design §2), `LANGUAGE plpgsql STABLE`, granted to `lde_app`.
    - `change_sets_guard()` exactly as design §2, with triggers `change_sets_guard` (`BEFORE INSERT OR UPDATE OR DELETE … FOR EACH ROW`) and `change_sets_no_truncate` (`BEFORE TRUNCATE … FOR EACH STATEMENT`). Messages name the change set id and its state.
    - RLS enabled and forced, `tenant_isolation`, and `owner_maintenance` via `format(… %I, current_user)`, as in `0006`.
    - `GRANT SELECT, INSERT, UPDATE (title, reason, items, state, decided_by, decided_at, decision_reason) ON change_sets TO lde_app`.
    - Header comment in the style of `0007`, with a `-- SPEC-007-Rnn` line above each object.
  - Verify: `make test-integration TEST_DB=…` passes, including 1.1.
  - Integrity: yes
- [x] 1.3 Test: a revision keeps every prior value (SPEC-007-R01)
  - Requirements: SPEC-007-R01
  - Files: `internal/storage/integration_test.go`
  - Do: add `TestRevisionsKeepPriorValues` with a `newTenant`. All statements run as `as(tenant)`.
    1. Insert node `S`: `INSERT INTO lineage_nodes (tenant_id, kind, label, attrs, created_by) VALUES ($1, 'sample', 'Plasma 042', '{"volume_ul": 50}', 'alice') RETURNING id`.
    2. In a new transaction, call `commitChangeSet(…, "alice", "pipetting error")`, then `UPDATE lineage_nodes SET attrs = '{"volume_ul": 45}' WHERE id = S`.
    3. In a third transaction, inside a fresh `commitChangeSet`, run `UPDATE lineage_nodes SET label = label WHERE id = S`. This is a no-op.
    4. Read `SELECT rev, value, author, reason, change_set_id FROM record_revisions WHERE entity_type = 'lineage_node' AND entity_id = S ORDER BY rev`.
    - Want exactly two rows:
      - Rev 1: value `{"label": "Plasma 042", "attrs": {"volume_ul": 50}}`, author `alice`, reason `created`, change set NULL.
      - Rev 2: value with `45`, author `alice`, reason `pipetting error`, change set = the step-2 id.
    - The current row's `attrs` is `{"volume_ul": 45}`.
    - Compare values by unmarshalling into `map[string]any`.
    - Comment `// SPEC-007-R01`.
  - Verify: fails only on the new test.
  - Integrity: yes
- [x] 1.4 Test: revisable fields change only in a committing change set (SPEC-007-R02)
  - Requirements: SPEC-007-R02
  - Files: `internal/storage/integration_test.go`
  - Do: add `TestRevisableFieldsNeedChangeSet`. Use `newSealFixture(t, db, "completed")`: its run is unsealed and its nodes are `f.s` and so on. Look up the experiment id with `SELECT experiment_id FROM runs WHERE id = f.runID`. Each case below runs in its own `WithTenant` as `as(f.tenant)`:
    1. With no change set, `UPDATE lineage_nodes SET attrs = '{}' WHERE id = f.s` fails with `LDE03`.
    2. With no change set, `UPDATE runs SET params = '{"x": 1}' WHERE id = f.runID` fails with `LDE03`.
    3. With no change set, `UPDATE experiments SET title = 'x' WHERE id = <exp>` fails with `LDE03`.
    4. After `proposeChangeSet` plus `set_config('app.change_set_id', <proposed id>, true)`, the case 1 statement fails with `LDE03`.
    5. Commit a change set in one transaction. In a second transaction, `set_config` its id, then run the case 1 statement: it fails with `LDE03`.
    6. Insert a change set with `reason = '  '` and `state = 'committed'` directly (no helper): it fails with `23514`.
    7. With no change set, `UPDATE runs SET state = 'running' WHERE id = f.runID` succeeds. The database doesn't check state transitions, and `state` isn't revisable.
    8. Inside `commitChangeSet`, the case 1 statement succeeds.
    - Comment `// SPEC-007-R02`.
  - Adjusted: the case 1 statement sets `attrs = '{"x": 1}'` rather than `'{}'`, because the fixture node's attrs are already `{}` and an unchanged value writes no revision and needs no change set.
  - Verify: fails only on the new tests (1.3 and 1.4).
  - Integrity: yes
- [x] 1.5 Test: revisions are append-only for every role (SPEC-007-R03)
  - Requirements: SPEC-007-R03
  - Files: `internal/storage/integration_test.go`
  - Do: add `TestRevisionsAreAppendOnly`. In a new tenant, insert a node as `as(tenant)` so it has revision 1. Then check with `wantCode`:
    - As the owner (`db.Pool.Exec`), `UPDATE record_revisions SET reason = 'x' WHERE tenant_id = $1` fails with `LDE03`.
    - As the owner, `DELETE FROM record_revisions WHERE tenant_id = $1` fails with `LDE03`.
    - As the owner, `TRUNCATE record_revisions` fails with `LDE03`.
    - As `lde_app`, `INSERT INTO record_revisions (tenant_id, entity_type, entity_id, rev, value, author, reason) VALUES ($1, 'lineage_node', <node>, 9, '{}', 'x', 'x')` fails with `42501`.
    - Comment `// SPEC-007-R03`.
  - Verify: fails only on the new tests (1.3, 1.4 and 1.5).
  - Integrity: yes
- [x] 1.6 Migration `0012_record_revisions`
  - Requirements: SPEC-007-R01, SPEC-007-R02, SPEC-007-R03, SPEC-007-R10
  - Files: `internal/storage/migrations/0012_record_revisions.sql`, `internal/storage/integration_test.go` (only the three existing tests named below)
  - Do:
    - In this order:
      1. Create the `record_revisions` table (design §2), with its `entity_type` `CHECK`.
      2. Backfill revision 1 for `lineage_nodes` (`jsonb_build_object('label', label, 'attrs', attrs)`), `runs` (`jsonb_build_object('params', params)`) and `experiments` (`jsonb_build_object('title', title)`). Each has author `created_by`, reason `'created'`, change set NULL and `created_at`.
      3. Create `record_revise()` exactly as design §2.
      4. Attach the triggers `lineage_nodes_revisions` (`record_revise('lineage_node', 'label', 'attrs')`), `runs_revisions` (`record_revise('run', 'params')`) and `experiments_revisions` (`record_revise('experiment', 'title')`). Each is `AFTER INSERT OR UPDATE … FOR EACH ROW`.
      5. Create `record_revisions_reject_mutation()` and the triggers `record_revisions_append_only` and `record_revisions_no_truncate`.
      6. Create `runs_params_sealed()` and its trigger (design §2).
      7. RLS, policies and `GRANT SELECT ON record_revisions TO lde_app`.
      8. `GRANT UPDATE (label, attrs) ON lineage_nodes TO lde_app` and `GRANT UPDATE (title) ON experiments TO lde_app`.
    - `record_revise()` is owned by the migrating role and is `SECURITY DEFINER`. Don't grant `INSERT` on `record_revisions` to anyone.
    - Existing test changes, as SPEC-007 amends them (design §4):
      - `TestAppRoleCannotUpdateUnusedTables`: the table becomes these statements and expected codes:
        - `UPDATE experiments SET slug = 'x'` → `42501`
        - `UPDATE lineage_nodes SET kind = 'analysis'` → `42501`
        - `UPDATE instruments SET name = 'x'` → `42501`
        - `UPDATE experiments SET title = 'x'` → `LDE03`
        - `UPDATE lineage_nodes SET label = 'x'` → `LDE03`
        - `UPDATE runs SET state = 'running'` → `""`
      - `TestSealedNodesAreImmutable`: the "update unsealed sample" case now runs as `lde_app` (`db.WithTenant(…, as(f.tenant), …)`), after `commitChangeSet(ctx, tx, f.tenant, "it@lab", "fixture")` in the same transaction, and still expects success. The other three cases stay owner-run and unchanged.
      - `TestSigningChangesNothingElse`: the "it@lab signs and edits params" case runs `commitChangeSet(ctx, tx, tenant, "it@lab", "correct params at sign-off")` in the same transaction before the statement, and still expects success. Change only that case's `exec` call. Add a small inline closure if needed.
  - Added after integrity review (FAIL, round 1): `record_revise()` uses `search_path = pg_catalog, public, pg_temp` and qualifies every relation as `public.<name>`, so `lde_app` can't shadow `change_sets` or `record_revisions` with temp tables. One extra case in `TestRevisableFieldsNeedChangeSet` covers it. Design §2 and the decision log are updated.
  - Verify: `make test-integration TEST_DB=…` passes, including 1.3, 1.4 and 1.5, and every existing test.
  - Integrity: yes
- [x] 1.7 Test: notes on sealed nodes are annotations (SPEC-007-R13)
  - Requirements: SPEC-007-R13
  - Files: `internal/storage/integration_test.go`
  - Do: add `TestNotesOnSealedNodes`. Start with `f := newSealFixture(t, db, "completed")`.
    1. As `as(f.tenant)`, insert note `n1` on `f.r`: `INSERT INTO notes (tenant_id, node_id, body, created_by) VALUES ($1, $2, 'Thawed at 09:00', 'alice') RETURNING id, annotation`. Want `annotation = false`.
    2. Seal: `db.Pool.Exec(UPDATE runs SET state = 'signed' WHERE id = f.runID)`.
    3. Insert note `n2` on `f.r` with body `Freezer alarm at 02:10, samples unaffected`. Want `annotation = true`, and `SELECT lineage_node_sealed(f.r)` still true.
    4. Inside `commitChangeSet`, `UPDATE notes SET body = 'Thawed at 09:15' WHERE id = n1` fails with `LDE04`.
    5. Inside `commitChangeSet`, `UPDATE notes SET body = 'Freezer alarm at 02:10; checked, samples unaffected' WHERE id = n2` succeeds.
    6. As the owner, `UPDATE notes SET annotation = true WHERE id = n1` fails with `LDE03`.
    - Comment `// SPEC-007-R13`.
  - Verify: fails only on the new test.
  - Integrity: yes
- [x] 1.8 Migration `0013_notes`
  - Requirements: SPEC-007-R13, SPEC-007-R01
  - Files: `internal/storage/migrations/0013_notes.sql`
  - Do:
    - `notes` (design §2) with index `(tenant_id, node_id, created_at)`.
    - `notes_guard()` and the triggers `notes_guard` and `notes_no_truncate` (design §2). The run lock copies the `PERFORM 1 FROM runs r … FOR SHARE OF r` pattern from `lineage_edges_reject_sealed()`, applied to `node_id`: the run whose `node_id` it is, and the runs it `wasGeneratedBy`.
    - The trigger `notes_revisions`: `AFTER INSERT OR UPDATE … EXECUTE FUNCTION record_revise('note', 'body')`.
    - The trigger `notes_role_gate`: `BEFORE INSERT OR UPDATE OR DELETE … FOR EACH STATEMENT EXECUTE FUNCTION lab_records_require_scientist()`.
    - RLS, policies, and `GRANT SELECT, INSERT, UPDATE (body) ON notes TO lde_app`.
  - Verify: `make test-integration TEST_DB=…` passes, including 1.7.
  - Integrity: yes
- [x] 1.9 IQ trigger list
  - Requirements: SPEC-007-R01, SPEC-007-R03, SPEC-007-R06, SPEC-007-R13 (installation check)
  - Files: `cmd/server/iq.go`
  - Do: append these to `iqTriggers`:
    - `change_sets_guard`, `change_sets_no_truncate`
    - `lineage_nodes_revisions`, `runs_revisions`, `experiments_revisions`, `notes_revisions`
    - `record_revisions_append_only`, `record_revisions_no_truncate`
    - `runs_params_sealed`
    - `notes_guard`, `notes_no_truncate`, `notes_role_gate`
  - Verify: `LDE_DATABASE_URL=postgres://lde:lde@localhost:5432/lde_spec007?sslmode=disable go run ./cmd/server iq` passes.
  - Integrity: yes

## Phase 2: Domain

- [x] 2.1 Diff and blame
  - Requirements: SPEC-007-R07, SPEC-007-R08, SPEC-007-R09
  - Files: `internal/domain/revision.go` (new)
  - Do:
    - `type Revision struct { Rev int; Value map[string]any; Author, Reason string; ChangeSetID *string; CreatedAt time.Time }`. JSON tags: `rev`, `value`, `author`, `reason`, `change_set_id`, `created_at`.
    - `type Change struct { Path string; Kind string; From, To any }`. JSON tags: `path`, `kind`, `from,omitempty`, `to,omitempty`. `Kind` is `"added"`, `"removed"` or `"changed"`.
    - `func Diff(from, to map[string]any) []Change`, with the algorithm and path syntax from design §2 "Paths in diff and blame". Write a `jsonPath(parent, key string) string` helper and an index form `parent + "[" + strconv.Itoa(i) + "]"`. Order: object keys sorted, array indexes ascending.
    - `type BlameEntry struct { Path string; Rev int; Author string; At time.Time }`. JSON tags: `path`, `rev`, `author`, `at`.
    - `func Blame(revs []Revision) []BlameEntry` (revs oldest first):
      - Flatten each revision's value to `map[path]leaf`. A leaf is any non-container, or an empty map or slice.
      - Walk the revisions in order, keeping `map[path]{value, rev, author, at}`.
      - For each revision, set any path that is new or whose value differs (`reflect.DeepEqual`) to this revision, and delete paths absent from it.
      - Return the final map sorted by path.
      - With no revisions, return an empty slice.
    - Doc comments as in `lineage.go`. No tests (R07–R09 are `Test: no`).
  - Verify: `go build ./...`, `go vet ./...`.

## Phase 3: API

- [x] 3.1 Change set core: items, apply, direct-edit helper
  - Requirements: SPEC-007-R04, SPEC-007-R11
  - Files: `internal/api/changesets.go` (new)
  - Do:
    - `type changeItem struct` with fields `Op, NodeID, RunID, ExperimentID, NoteID string`; `Kind domain.NodeKind`; `Label *string`; `Attrs map[string]any`; `Params json.RawMessage`; `Title, Body, From, To string`; `Relation domain.Relation`.
      - JSON tags are snake case with `omitempty`: `op`, `node_id`, `run_id`, `experiment_id`, `note_id`, `kind`, `label`, `attrs`, `params`, `title`, `body`, `from`, `to`, `relation`.
    - `func (it changeItem) validate() error` returns `badRequest` for anything invalid, per the design §2 item table:
      - every id must pass `validUUID`
      - `update_node` needs `label` or `attrs`, and a given label must be non-empty
      - `params` must decode to a JSON object
      - titles and bodies must not be blank after `strings.TrimSpace`
      - `kind` and `relation` must pass `Valid()`
      - an unknown `op` is rejected
    - `func validateItems(items []changeItem) error`: needs 1..100 items, each valid. Error messages say `item %d: …`.
    - `func latestRev(ctx, tx, entityType, id string) (int, error)`: `SELECT COALESCE(max(rev), 0) FROM record_revisions WHERE entity_type = $1 AND entity_id = $2`.
    - `func applyItems(ctx, tx pgx.Tx, p Principal, csID, reason string, items []changeItem) error`:
      - Run `SELECT set_config('app.change_set_id', $1, true)` first.
      - Then apply each item, in order, with `applyItem`.
    - `func applyItem(ctx, tx, p, csID, reason string, it changeItem) error`:
      - **Update ops** (`update_node`, `update_run_params`, `set_experiment_title`, `edit_note`):
        - Read `before := latestRev`, then run the `UPDATE … WHERE id = $1`.
        - `update_node` uses `label = COALESCE($2, label), attrs = COALESCE($3, attrs)`, passing nil for absent fields.
        - If `RowsAffected() == 0`, return `notFound("<entity>")`.
        - Read `after := latestRev`. If `after > before`, write `audit.Append` `record.revised`: entity type and id of the record, `Reason: reason`, payload `{"rev": after, "change_set_id": csID}`.
      - **`add_node`, `add_edge`:** call the existing `insertNode` and `insertEdge`.
      - **`add_note`:** call `insertNote` (task 3.2). Declare it here as a forward reference in the same package, and write it in 3.2.
    - `const changeSetColumns`, `type changeSet struct` (design §2 JSON), and `scanChangeSet(row pgx.Row) (changeSet, error)`. Select `decided_by` as `(SELECT handle FROM principals WHERE id = c.decided_by)` and `items_sha256` as `encode(items_sha256, 'hex')`.
    - `func directEdit(ctx, tx, p Principal, entityType, entityID, reason string, it changeItem) (editResult, error)`:
      - `type editResult struct { ChangeSetID string \`json:"change_set_id"\`; Rev int \`json:"rev"\` }`.
      - Insert the change set: `INSERT INTO change_sets (tenant_id, title, reason, author, state, items, decision_reason) VALUES ($1, 'Edit ' || $2, $3, $4, 'committed', $5, $3) RETURNING id, encode(items_sha256, 'hex')`, with items `[it]` marshalled.
      - Write `audit.Append` `change_set.committed`: entity `change_set`, `Reason: reason`, payload `{"items_sha256": …, "items": 1}`.
      - Then `applyItems`, then `latestRev(entityType, entityID)` for the result.
  - Verify: `go build ./...` (task 3.2 lands in the same commit if `insertNote` is needed to build: implement 3.1 and 3.2 back to back and commit them together).
- [x] 3.2 Direct-edit and notes endpoints
  - Requirements: SPEC-007-R11, SPEC-007-R13
  - Files: `internal/api/edits.go` (new), `internal/api/server.go` (routes only)
  - Do:
    - `insertNote(ctx, tx, p, nodeID, body string) (note, error)`:
      - `INSERT INTO notes (tenant_id, node_id, body, created_by) … RETURNING id, node_id, body, annotation, created_by, created_at`.
      - Write `audit.Append` `note.created`: entity `note`, payload `{"node_id", "annotation"}`.
      - `type note struct` per the design note JSON, with `Rev int` set to 1 on insert.
    - Handlers. Each one decodes with `decodeJSON`, returns 400 for a blank `reason` before touching the DB, returns `notFound` for an invalid uuid path, runs in `WithTenant(…, p.As, …)`, and replies with `writeJSON`:
      - `patchNode`: `PATCH /v1/lineage/nodes/{id}`, body `{label *string, attrs map[string]any, reason}`. Calls `directEdit("lineage_node", …, changeItem{Op: "update_node", …})` after `validate()`, and returns 200 with `editResult`.
      - `patchRunParams`: `PATCH /v1/runs/{id}/params`, body `{params json.RawMessage, reason}`. Uses op `update_run_params`.
      - `patchExperiment`: `PATCH /v1/experiments/{id}`, body `{title, reason}`. Uses op `set_experiment_title`.
      - `patchNote`: `PATCH /v1/notes/{id}`, body `{body, reason}`. Uses op `edit_note`.
      - `createNote`: `POST /v1/lineage/nodes/{id}/notes`, body `{body}`. Returns 400 if the body is blank, else 201 with the note.
      - `listNotes`: `GET /v1/lineage/nodes/{id}/notes` returns `{"items": [...]}` oldest first. `rev` is `(SELECT max(rev) FROM record_revisions r WHERE r.entity_type = 'note' AND r.entity_id = n.id)`.
    - Register all six routes in `Handler()` beside the existing lineage, run and experiment routes.
  - Verify: `go build ./...`. Then `make run` against `lde_spec007` with dev headers:
    - Create a node, then `PATCH` its attrs with a reason: you get `rev` 2.
    - Without a reason you get 400.
    - `GET /v1/audit/events` shows `change_set.committed` then `record.revised`.
    - Add a note, list it, and edit it to get `rev` 2.
- [x] 3.3 Change set create, list, get and replace
  - Requirements: SPEC-007-R12, SPEC-007-R05
  - Files: `internal/api/changesets.go`, `internal/api/server.go` (routes only)
  - Do:
    - `createChangeSet`: `POST /v1/change-sets`, body `{title, reason string; items []changeItem}`.
      - A blank title or reason is 400, and so is `validateItems`.
      - Insert with `state` `proposed` and `author` `p.Actor`, `RETURNING` the columns.
      - Write `change_set.proposed`: payload `{items_sha256, items: len}`, `Reason: reason`.
      - Return 201 with the change set.
    - `listChangeSets`: `GET /v1/change-sets?state=&source=&cursor=&limit=`, using `readPage`.
      - Filters are optional. An unknown state or source value is 400.
      - Order by `created_at, id`, with a cursor like `listExperimentRuns`.
      - Summary fields: `id, title, reason, source, author, state, items` (count, `jsonb_array_length`), `created_at`.
      - Return a `page`.
    - `getChangeSet`: `GET /v1/change-sets/{id}` returns the change set plus `current []json.RawMessage`. Each entry is the latest revision `value` of the item's target, or `null` for ops that create things:
      - `update_node` → `lineage_node`/`node_id`
      - `update_run_params` → `run`/`run_id`
      - `set_experiment_title` → `experiment`/`experiment_id`
      - `edit_note` → `note`/`note_id`
    - `replaceChangeSet`: `PUT /v1/change-sets/{id}`, same body and checks as create. It runs `UPDATE change_sets SET title, reason, items WHERE id = $1 RETURNING …`, mapping 0 rows to 404. Write `change_set.updated`. The DB guard gives 403 for a non-author and 409 for a terminal state.
    - Register the four routes.
  - Verify: `go build ./...`. With dev headers, create, list, get and replace a change set: each works, and replacing as another dev actor is 403.
- [x] 3.4 Test: proposals are invisible until committed (SPEC-007-R05)
  - Requirements: SPEC-007-R05
  - Files: `internal/api/integration_test.go`
  - Do: add `TestProposedChangesInvisible`, using `newFixture`, `prodServer` and `delegateAliceToBot`. Tokens `alice := mintToken(…"alice")` and `bot := mintToken(…"bot")`.
    1. Alice creates `S` (`POST /v1/lineage/nodes` `{"kind": "sample", "label": "Plasma 042", "attrs": {"volume_ul": 50}}`) and `R` (`{"kind": "analysis", "label": "R"}`), then edge `S →used→ R`.
    2. Bot, with `forAlice`, posts `/v1/change-sets` `{"title": "relabel", "reason": "naming convention", "items": [{"op": "add_node", "kind": "sample", "label": "N-042"}, {"op": "update_node", "node_id": S, "label": "P-042"}]}`. Want 201 and state `proposed`.
    3. Before commit:
       - `GET /v1/lineage/{S}?direction=both` (alice) shows S labelled `Plasma 042` and no node labelled `N-042`.
       - `GET /v1/lineage/{R}` upstream has no `N-042`.
       - `SELECT count(*) FROM lineage_nodes WHERE label = 'N-042'` via `db.WithTenant(…, storage.Principal{}, …)` is 0.
    4. Alice posts `/v1/change-sets/{id}/commit` `{"reason": "ok"}`. Want 200 and state `committed`.
    5. After: S is labelled `P-042` and the count is 1.
    - Comment `// SPEC-007-R05`.
  - Verify: `make test-integration TEST_DB=…` fails only on the new test (commit is 404).
- [x] 3.5 Test: a commit applies everything or nothing (SPEC-007-R04)
  - Requirements: SPEC-007-R04
  - Files: `internal/api/integration_test.go`
  - Do: add `TestCommitIsAtomic`. Use the dev server as in `TestRunPinsProtocolVersion`, with `send(t, srv, "", devHeaders, …)` where `devHeaders = {"X-LDE-Tenant": tenant, "X-LDE-Actor": "it@lab"}`.
    1. Create `S` (`attrs {"volume_ul": 50}`) and `R`.
    2. Propose items `[{"op": "update_node", "node_id": S, "attrs": {"volume_ul": 45}}, {"op": "add_edge", "from": S, "to": R, "relation": "used"}, {"op": "add_edge", "from": R, "to": S, "relation": "used"}]`.
    3. Record `n := auditCount(…)`.
    4. Commit: want 409.
    5. Check:
       - `auditCount` is still `n`.
       - `SELECT max(rev) FROM record_revisions WHERE entity_id = S` is 1.
       - `SELECT attrs->>'volume_ul' FROM lineage_nodes WHERE id = S` is `50`.
       - `SELECT count(*) FROM lineage_edges WHERE from_id = S` is 0.
       - `GET /v1/change-sets/{id}` state is still `proposed`.
    - Comment `// SPEC-007-R04`.
  - Verify: fails only on the new tests.
- [x] 3.6 Test: committing a change to a sealed record is 409 (SPEC-007-R10)
  - Requirements: SPEC-007-R10
  - Files: `internal/api/integration_test.go`
  - Do: add `TestSealedCommitConflicts` (dev server, it@lab).
    1. Create an experiment and two runs, `R` and `A`.
    2. Move `R` planned → running → completed → signed (reason `approved`) with `POST /v1/runs/{id}/transitions`.
    3. Abort `A` (reason `contaminated`).
    4. For each run, propose `[{"op": "update_run_params", "run_id": <run>, "params": {"temp_c": 37}}]`: want 201. Then commit: want 409.
    - Comment `// SPEC-007-R10`.
  - Verify: fails only on the new tests.
- [x] 3.7 Commit, reject and withdraw
  - Requirements: SPEC-007-R04, SPEC-007-R05, SPEC-007-R06, SPEC-007-R10, SPEC-007-R12
  - Files: `internal/api/changesets.go`, `internal/api/server.go` (routes only)
  - Do: `decideChangeSet(state string) http.HandlerFunc` serves `POST /v1/change-sets/{id}/commit|reject|withdraw` (`committed`, `rejected`, `withdrawn`). The body is `{reason}`, and a blank reason is 400. In one `WithTenant`:
    1. `SELECT … FROM change_sets c WHERE c.id = $1 FOR UPDATE`. No row is 404. A state other than `proposed` is 409, with `change set is <state>`.
    2. `UPDATE change_sets SET state = $2, decision_reason = $3 WHERE id = $1 RETURNING …`. The guard enforces agents (403), the author rule (403) and roles.
    3. Write `audit.Append` `change_set.<committed|rejected|withdrawn>`: `Reason: reason`, payload `{items_sha256, items: n}`.
    4. For a commit only: unmarshal `items` into `[]changeItem`, then `applyItems(ctx, tx, p, id, <the change set's own reason>, items)`.
    5. Return 200 with the change set as re-read after the update.
  - Verify: `make test-integration TEST_DB=…` passes, including 3.4, 3.5 and 3.6.
- [x] 3.8 Revision history endpoints
  - Requirements: SPEC-007-R07, SPEC-007-R08, SPEC-007-R09
  - Files: `internal/api/revisions.go` (new), `internal/api/server.go` (routes only)
  - Do:
    - `var revisableTypes = map[string]bool{"lineage_node": true, "run": true, "experiment": true, "note": true}`. An unknown `{type}` or an invalid `{id}` is 404.
    - `loadRevisions(ctx, tx, type, id string) ([]domain.Revision, error)`: `SELECT rev, value, author, reason, change_set_id, created_at FROM record_revisions WHERE entity_type = $1 AND entity_id = $2 ORDER BY rev`. Zero rows is `notFound("record")`.
    - `GET /v1/records/{type}/{id}/revisions` returns `{"items": revs}`.
    - `GET /v1/records/{type}/{id}/revisions/{rev}`: `rev` must be a positive int, else 400. A missing revision is 404.
    - `GET /v1/records/{type}/{id}/diff?from=&to=`: both are required positive ints, else 400. A revision that doesn't exist is 404. Returns `{"from", "to", "changes": domain.Diff(a.Value, b.Value)}`, with `changes` never null (use `[]domain.Change{}`).
    - `GET /v1/records/{type}/{id}/blame` returns `{"rev": <latest>, "fields": domain.Blame(revs)}`.
    - Register the four routes.
  - Verify: `go build ./...`. With dev headers, after editing a node's attrs twice:
    - `revisions` lists 3.
    - `diff?from=1&to=3` shows `$.attrs.volume_ul` `changed`.
    - `blame` attributes `$.label` to rev 1 and the edited attr to rev 3.

## Phase 4: UI

- [x] 4.1 API client for edits, notes, change sets and revisions
  - Requirements: SPEC-007-R07, SPEC-007-R08, SPEC-007-R09, SPEC-007-R11, SPEC-007-R12 (UI support)
  - Files: `web/src/api.ts`
  - Do:
    - Widen `request`'s method to `'GET' | 'POST' | 'PUT' | 'PATCH'`, and add `put` and `patch` beside `post`.
    - Types:
      - `Revision`, `Change`, `BlameEntry`, `EditResult`, `Note`
      - `ChangeItem`, a union keyed on `op` per the design §2 item table
      - `ChangeSet`, `ChangeSetDetail` (`ChangeSet & { current: unknown[] }`), `ChangeSetSummary`
      - `ChangeSetState`, `ChangeSetSource`
    - Functions:
      - `editNode(id, {label?, attrs?}, reason)`, `editRunParams(id, params, reason)`, `editExperimentTitle(id, title, reason)`, `editNote(id, body, reason)`
      - `listNotes(nodeId)`, `addNote(nodeId, body)`
      - `listRevisions(type, id)`, `diffRevisions(type, id, from, to)`, `blame(type, id)`
      - `listChangeSets({state?, source?, cursor?})`, `getChangeSet(id)`, `proposeChangeSet(title, reason, items)`, `decideChangeSet(id, 'commit' | 'reject' | 'withdraw', reason)`
    - `RecordType = 'lineage_node' | 'run' | 'experiment' | 'note'`.
  - Verify: `cd web && npm run build`.
- [x] 4.2 Edit with reason, and the "changed" marker
  - Requirements: SPEC-007-R11 (UI)
  - Files: `web/src/EditField.tsx` (new), `web/src/RunView.tsx`, `web/src/ExperimentView.tsx`, `web/src/LineageView.tsx`, `web/src/styles.css`
  - Do:
    - `EditField` props: `{ label: string; value: string; multiline?: boolean; json?: boolean; changed: boolean; onSave: (value: string, reason: string) => Promise<void> }`.
      - At rest it shows the value, an "Edit" button and, when `changed`, a "changed" button. That button dispatches `window.dispatchEvent(new CustomEvent('labrat:show-revisions'))`.
      - Editing shows an input (or a textarea when `multiline` or `json`), a required "Reason" input, and Save and Cancel.
      - Save is disabled while the reason is blank. With `json`, the value must `JSON.parse` to an object, or an inline error shows.
      - API errors show in `role="alert"`. On success it leaves edit mode.
    - `RunView`: params use `EditField` (`json`) and `editRunParams`. `changed` is true when `blame('run', id)` has any field with `rev > 1`. Reload after save.
    - `ExperimentView`: title uses `editExperimentTitle`, with `changed` from `blame('experiment', id)`.
    - `LineageView`'s selected-node panel: label and attrs (`json`) use `editNode`, with `changed` from `blame('lineage_node', nodeId)`. A field counts as changed when a blame path starts with `$.label` or `$.attrs` and has `rev > 1`.
    - Keep the existing layout. All controls are reachable by keyboard and labelled.
  - Verify: `npm run build`. With dev sign-in:
    - Edit run params with a reason: the marker appears and the audit viewer shows `record.revised`.
    - Saving with a blank reason is impossible.
- [x] 4.3 Revisions panel
  - Requirements: SPEC-007-R07, SPEC-007-R08, SPEC-007-R09 (UI)
  - Files: `web/src/BottomPanel.tsx`, `web/src/styles.css`
  - Do:
    - `PANELS` becomes `['Problems', 'History', 'Revisions']`. The new `Revisions` component maps tab kinds to record types: experiment → `experiment`, run → `run`, lineage → `lineage_node`. Other kinds show "Select a record to see its revisions".
    - It lists revisions (rev, time, author, reason) as a table. Two radio columns, "From" and "To", default to the previous and the latest revision.
    - It shows `diffRevisions` as a table of path, kind, from and to (JSON-stringified). Each path cell's `title` is `rev N · author · time` from `blame`, when the path is in the blame result.
    - `BottomPanel` listens for `labrat:show-revisions` and switches to `Revisions`.
    - Refetch when the active tab changes, and on a `labrat:revised` window event, which `EditField` dispatches after a successful save. Add that dispatch in `EditField`.
  - Verify: `npm run build`. Edit a node twice, then compare revisions 1 and 3 in the panel. Use the keyboard only.
- [x] 4.4 Notes under the run view
  - Requirements: SPEC-007-R13 (UI)
  - Files: `web/src/Notes.tsx` (new), `web/src/RunView.tsx`, `web/src/styles.css`
  - Do:
    - `Notes` props: `{ nodeId: string }`. It lists `listNotes(nodeId)` oldest first.
      - Each body is rendered with `markdown-it` (`html: false`), configured as in `ExperimentView`.
      - It shows author and time, "Annotation (added after sealing)" when `annotation`, and "edited (rev N)" when `rev > 1`.
    - "Add note": a textarea with a Preview toggle and a Save button that calls `addNote`.
    - Each note has "Edit": a textarea plus a required reason, which calls `editNote`. A 409 shows the server message in `role="alert"` (for example, a note that is part of a sealed run).
    - `RunView` renders `<Notes nodeId={run.node_id} />` in a `<section>` headed "Notes" after the existing sections.
  - Verify: `npm run build`.
    - Add a note to a completed run, sign the run, and add another: the second is labelled an annotation.
    - Editing the first shows the 409 message.
- [x] 4.5 Review inbox
  - Requirements: SPEC-007-R05, SPEC-007-R06, SPEC-007-R12 (UI)
  - Files: `web/src/InboxView.tsx` (new), `web/src/tabs.ts`, `web/src/Workspace.tsx`, `web/src/Explorer.tsx`, `web/src/CommandPalette.tsx` (or wherever palette entries are listed)
  - Do:
    - Add the tab kind `inbox` to `Kind`, `KINDS` and `SINGLETONS` (`{ kind: 'inbox', id: 'inbox', title: 'Review inbox' }`). Render `InboxView` for it in `Workspace`, the same way the other singleton views are rendered.
    - Add an explorer entry and a palette entry, "Review inbox", next to "Audit trail".
    - `InboxView` loads `listChangeSets({state: 'proposed'})`, follows `next_cursor` until it is null, and groups the change sets under the headings Human, Parser and Agent.
    - Each change set shows its title, author, reason and created time. Expanding it calls `getChangeSet`, and each item renders as follows:
      - **Update ops:** a table of field, current and proposed, where current comes from `current[i]`, JSON-stringified.
      - **Ops that create things:** a one-line summary, e.g. "add sample N-042" or "add edge A →used→ B".
    - Commit and Reject each open a required reason input and call `decideChangeSet`. Withdraw is shown only when `author` equals `getMe()`'s `actor`.
    - After a decision, the list reloads. API errors (403 for agents, 409 for sealed records) show in `role="alert"`.
  - Verify: `npm run build`.
    - As dev actor `alice`, `curl` a proposal from dev actor `bob`. It appears in alice's inbox.
    - Committing shows the change in the lineage view, and the audit trail shows `change_set.committed`.

## Phase 5: Docs

- [x] 5.1 `docs/revisions.md` and README
  - Requirements: SPEC-007-R07, SPEC-007-R11, SPEC-007-R12 (documentation)
  - Files: `docs/revisions.md` (new), `README.md`
  - Do:
    - `docs/revisions.md` covers:
      - what is revisable
      - that every edit is a change set with a reason
      - the change set lifecycle and who can do what: agents and services propose only, the author replaces or withdraws, a scientist commits or rejects
      - that sealed records can't be changed until SPEC-008, and that notes on sealed records are annotations
      - the endpoints, with one `curl` example each for a direct edit, a proposal, a commit and the diff
    - README: update the status or feature row for revisions and link the doc.
  - Verify: links resolve.

## Phase 6: Verify and close out

- [x] 6.1 `verifier` passes the whole spec
  - [x] 6.1a Record the `notes_reject_mutation()` name and the `search_path` hardening of `notes_reject_mutation()` and `runs_params_sealed()` (and their two test cases) in design.md decision log (verifier round 1, check 4). Files: `design.md`.
- [x] 6.2 `uat-planner` writes [uat.md](uat.md)
- [x] 6.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated
- [x] 6.4 Summary commit written as the PR description
