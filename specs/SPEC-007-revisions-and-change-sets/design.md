# SPEC-007 design: Revisions, change sets and notes

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one generic `record_revisions` table, filled by one `AFTER INSERT OR UPDATE` trigger function on the four revisable tables. The trigger reads `app.change_set_id`, which the API sets for the transaction, and takes the author and reason from that change set's row. `change_sets` holds proposed items as JSON. Commit replays the items through the same Go code as direct edits. Diff and blame are pure Go in `internal/domain`.
- Not doing: per-table history tables, an event-sourcing framework, three-way merge, storing diffs instead of snapshots, or references between items of one change set (an `add_edge` names existing node ids only).

## 2. Data and interfaces

### Tables

`change_sets` (migration `0011_change_sets`):

| Column | Type | Notes |
|---|---|---|
| `id` | uuid PK | `UNIQUE (tenant_id, id)` |
| `tenant_id` | uuid | FK tenants |
| `title` | text | `CHECK (btrim(title) <> '')` |
| `reason` | text | `CHECK (btrim(reason) <> '')`; becomes every revision's reason |
| `source` | text | `human`, `parser` or `agent`; default `human`; stamped by the DB from the author's principal kind (human → human, agent → agent, service → parser) |
| `author` | text | the actor string, e.g. `alice via bot` |
| `author_id` | uuid | FK principals; stamped by the DB as `lde_current_principal()` |
| `state` | text | `proposed`, `committed`, `rejected`, `withdrawn` |
| `items` | jsonb | `CHECK (jsonb_typeof(items) = 'array')` |
| `items_sha256` | bytea | `GENERATED ALWAYS AS (protocol_sha256(items::text)) STORED` |
| `decided_by` | uuid | FK principals; stamped by the DB |
| `decided_at` | timestamptz | stamped by the DB |
| `decision_reason` | text | required once decided |
| `created_at` | timestamptz | stamped by the DB |

Checks: `(state = 'proposed') = (decided_at IS NULL)`, `(decided_at IS NULL) = (decided_by IS NULL)`, `(state = 'proposed') = (decision_reason IS NULL)`, and `decision_reason IS NULL OR btrim(decision_reason) <> ''`. Index `(tenant_id, state, created_at)`.

`record_revisions` (migration `0012_record_revisions`): `(tenant_id, entity_type, entity_id uuid, rev int CHECK (rev > 0), value jsonb, author text, reason text, change_set_id uuid NULL, created_at timestamptz)`, PK `(tenant_id, entity_type, entity_id, rev)`, FK `(tenant_id, change_set_id)` → `change_sets (tenant_id, id)`. `entity_type ∈ {lineage_node, run, experiment, note}`, the same strings the audit trail uses.

`notes` (migration `0013_notes`): `(id, tenant_id, node_id, body text CHECK (btrim(body) <> ''), annotation bool NOT NULL DEFAULT false, created_by text, created_at)`, `UNIQUE (tenant_id, id)`, FK `(tenant_id, node_id)` → `lineage_nodes`.

### Revisable fields and revision values

A revision's `value` is an object holding the record's revisable fields only:

| Table | `entity_type` | `value` |
|---|---|---|
| `lineage_nodes` | `lineage_node` | `{"label": …, "attrs": {…}}` |
| `runs` | `run` | `{"params": {…}}` |
| `experiments` | `experiment` | `{"title": …}` |
| `notes` | `note` | `{"body": …}` |

Other columns don't make revisions. A run's state transition, for example, needs no change set.

### Triggers and functions

- **`record_revise()`** is `SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp`, with every relation written as `public.<name>`. Its arguments are the entity type, then the revisable column names. It runs as `AFTER INSERT OR UPDATE … FOR EACH ROW` and is attached as `lineage_nodes_revisions`, `runs_revisions`, `experiments_revisions` and `notes_revisions`.
  1. It builds `value` with `jsonb_object_agg(k, to_jsonb(NEW) -> k)` over the column names. On UPDATE, if the value is equal to OLD's, it returns without writing anything.
  2. If `app.change_set_id` is set, it looks up that change set in `NEW.tenant_id` with `state = 'committed' AND xmin = pg_current_xact_id()::xid`, meaning it was committed in this transaction (R02).
  3. On UPDATE, or on INSERT with the setting present, a missing committing change set raises `LDE03`: "`<entity>` `<id>` changed outside a change set committed in this transaction".
  4. It inserts revision `max(rev) + 1`. The author is the change set's `author`, or `NEW.created_by` when there is no change set. The reason is the change set's `reason`, or `created`. It also records the change set id and `clock_timestamp()`.
- **`record_revisions_reject_mutation()`** is attached as `record_revisions_append_only` (`BEFORE UPDATE OR DELETE`) and `record_revisions_no_truncate`. Each raises `LDE03` for every role, the owner included (R03).
- **`runs_params_sealed`** runs `BEFORE UPDATE OF params ON runs … WHEN (OLD.params IS DISTINCT FROM NEW.params)` and raises `LDE04` when `lineage_node_sealed(OLD.node_id)`. This covers aborted runs, which `runs_signed_immutable` doesn't (R10).
- **`lde_require_human()`** raises `LDE05` as `lde_app` unless the current principal is an enabled `human` (R06). For the owner it is a no-op, like `lde_require_role`.
- **`change_sets_guard()`** is attached as `BEFORE INSERT OR UPDATE OR DELETE … FOR EACH ROW`, plus `change_sets_no_truncate`. A DELETE or TRUNCATE raises `LDE03`.
  - **INSERT:** the state must be `proposed` or `committed` (a direct edit), otherwise `check_violation`. It calls `lde_require_role('scientist')`. As `lde_app` it stamps `author_id`, `source` and `created_at`.
  - **UPDATE:**
    - If `OLD.state <> 'proposed'`, it raises `LDE03`, because the decided states are terminal (R12).
    - A change to `id`, `tenant_id`, `source`, `author`, `author_id` or `created_at` raises `LDE03`.
    - If the new state is `proposed` or `withdrawn`, the current principal must be `OLD.author_id`, otherwise `LDE05` (R12).
    - If the new state isn't `proposed`, `title`, `reason` and `items` must be unchanged, otherwise `LDE03`. This way the items that were reviewed are the ones applied.
  - **Committed or rejected** (on INSERT or UPDATE): it calls `lde_require_human()` and `lde_require_role('scientist')`. As `lde_app` it stamps `decided_by` and `decided_at`. **Withdrawn:** it stamps the same two columns.
- **`notes_guard()`** is attached as `BEFORE INSERT OR UPDATE OR DELETE … FOR EACH ROW`, plus `notes_no_truncate`. A DELETE or TRUNCATE raises `LDE03`.
  - First it locks (`FOR SHARE`) the runs whose seal covers `node_id`, as in `0004`.
  - **INSERT:** it sets `NEW.annotation := lineage_node_sealed(NEW.node_id)` (R13).
  - **UPDATE:** a change to anything but `body` raises `LDE03`. If `NOT OLD.annotation AND lineage_node_sealed(OLD.node_id)`, it raises `LDE04` (R13).
  - A note on a sealed node is therefore an annotation, and annotations stay editable.
- **`notes_role_gate`** reuses `lab_records_require_scientist()` as a statement trigger.

### Grants and RLS

All three new tables get RLS enabled and forced, a `tenant_isolation` policy and an `owner_maintenance` policy, as in `0005`.

- `GRANT SELECT ON record_revisions TO lde_app`. Only the trigger writes revisions.
- `GRANT SELECT, INSERT, UPDATE (title, reason, items, state, decided_by, decided_at, decision_reason) ON change_sets TO lde_app`.
- `GRANT SELECT, INSERT, UPDATE (body) ON notes TO lde_app`.
- `GRANT UPDATE (label, attrs) ON lineage_nodes, UPDATE (title) ON experiments TO lde_app`. This re-grants, column by column, what SPEC-002 revoked, now behind the revision trigger. It amends SPEC-002-R08: see the decision log.
- `lde_app` already holds UPDATE on `runs`.
- Migration `0012` backfills revision 1 for every existing node, run and experiment, with author `created_by`, reason `created` and time `created_at`, before it creates the triggers.

### Change set items (v1)

Each item is a JSON object with an `op`:

| `op` | Fields | Applies |
|---|---|---|
| `update_node` | `node_id`, `label?`, `attrs?` (at least one; `attrs` replaces the whole object) | `UPDATE lineage_nodes` |
| `update_run_params` | `run_id`, `params` (object) | `UPDATE runs SET params` |
| `set_experiment_title` | `experiment_id`, `title` | `UPDATE experiments SET title` |
| `add_node` | `kind`, `label`, `attrs?` | `insertNode` |
| `add_edge` | `from`, `to`, `relation` | `insertEdge` |
| `add_note` | `node_id`, `body` | `insertNote` |
| `edit_note` | `note_id`, `body` | `UPDATE notes SET body` |

SPEC-013 adds `ingest_dataset`. An update that matches no row returns 404 and rolls back the whole commit.

### API

| Endpoint | Body | Returns |
|---|---|---|
| `POST /v1/change-sets` | `{title, reason, items}` | 201, the change set |
| `GET /v1/change-sets?state=&source=&cursor=&limit=` | | page of summaries, oldest first |
| `GET /v1/change-sets/{id}` | | the change set plus `current`: per item, the target's latest revision value (or null) |
| `PUT /v1/change-sets/{id}` | `{title, reason, items}` | the change set (author only, while proposed) |
| `POST /v1/change-sets/{id}/commit\|reject\|withdraw` | `{reason}` (required) | the change set |
| `PATCH /v1/lineage/nodes/{id}` | `{label?, attrs?, reason}` | `{change_set_id, rev}` |
| `PATCH /v1/runs/{id}/params` | `{params, reason}` | `{change_set_id, rev}` |
| `PATCH /v1/experiments/{id}` | `{title, reason}` | `{change_set_id, rev}` |
| `POST /v1/lineage/nodes/{id}/notes` | `{body}` | 201, the note |
| `GET /v1/lineage/nodes/{id}/notes` | | `{items: [note]}` oldest first, each with its latest `rev` |
| `PATCH /v1/notes/{id}` | `{body, reason}` | `{change_set_id, rev}` |
| `GET /v1/records/{type}/{id}/revisions` | | `{items: [revision]}` oldest first |
| `GET /v1/records/{type}/{id}/revisions/{rev}` | | one revision |
| `GET /v1/records/{type}/{id}/diff?from=&to=` | | `{from, to, changes: [{path, kind, from, to}]}` |
| `GET /v1/records/{type}/{id}/blame` | | `{rev, fields: [{path, rev, author, at}]}` |

- **Change set JSON:** `id, title, reason, source, author, state, items, items_sha256` (hex), `decided_by` (the decider's handle, or null), `decided_at, decision_reason, created_at`.
- **Revision JSON:** `rev, value, author, reason, change_set_id, created_at`.
- **Note JSON:** `id, node_id, body, annotation, created_by, created_at, rev`.
- **Direct edits (R11):** a direct edit inserts a change set with `state = 'committed'`, items `[item]`, `decision_reason = reason` and title `Edit <entity_type>`, then applies the item as a commit does. In the same transaction it writes `change_set.committed` and `record.revised`. Creating a note isn't an edit and makes no change set.
- **Errors:** `LDE03` and `LDE04` are 409 (sealed or terminal). `LDE05` is 403 (agent, service, non-author, or missing role). A blank reason is 400.
- **Paths in diff and blame:** `$` is the value root. A key matching `^[A-Za-z_][A-Za-z0-9_]*$` is written `.key`, any other key `["<JSON-escaped key>"]`, and an array index `[i]`.
  - Diff recurses into two objects key by key in sorted order, and into two arrays index by index. Any other pair that differs is `changed`.
  - A key or index present on one side only is `added` or `removed`, with its whole subtree as the value.
  - Blame attributes each leaf of the latest value. A leaf is a scalar, null, `{}` or `[]`. Each leaf gets the earliest revision from which it has held its current value without a break.

### Audit actions

- `change_set.proposed`, with payload `{items_sha256, items}` (items is the count).
- `change_set.updated`, written on PUT with the same payload.
- `change_set.committed`, with the same payload, also written for direct edits.
- `change_set.rejected` and `change_set.withdrawn`.
- `record.revised`: one per revision after the first, with entity type and id set to the record's, reason the change set's, and payload `{rev, change_set_id}`. A record's first revision is covered by its creation event: `lineage.node_created`, `run.created`, `experiment.created` or `note.created`.
- `note.created`, with payload `{node_id, annotation}`.

## 3. UI

- **Edit with reason:** every editable field has an edit control. Saving asks for a reason, which is required. This covers a node's label and attrs in the lineage view's selected-node panel, run params in the run view, and the experiment title in the experiment view. Saving calls the PATCH endpoint. A field whose blame shows a revision greater than 1 gets a "changed" marker. The marker is a button that opens the bottom panel's **Revisions** tab, through a `labrat:show-revisions` window event.
- **Revisions panel:** a new bottom-panel tab beside Problems and History. It shows the revision list for the active tab's record (experiment, run or lineage node) and lets you pick two revisions. Their diff from `/diff` is shown as a table of path, kind, from and to. Each changed path carries its blame (revision, author and time) in a `title` tooltip.
- **Notes:** a notes section under the run view, for the run's node. It lists notes oldest first, rendered with `markdown-it` (already a dependency). New notes are written in a textarea with a preview toggle. Annotations show an "Annotation (added after sealing)" label. Edit asks for a reason and shows "edited (rev N)".
- **Review inbox:** a singleton tab, `inbox`, opened from the explorer and the command palette. It lists proposed change sets grouped by source.
  - Each change set shows its items. An update item shows "current → proposed" per field, from `current`.
  - Commit and Reject buttons each need a reason. Withdraw appears only for the author.
  - Errors appear in a `role="alert"` region.

## 4. Testing

| Requirement | Test | Where |
|---|---|---|
| R01 | `TestRevisionsKeepPriorValues` | `internal/storage/integration_test.go` |
| R02 | `TestRevisableFieldsNeedChangeSet` | `internal/storage/integration_test.go` |
| R03 | `TestRevisionsAreAppendOnly` | `internal/storage/integration_test.go` |
| R06 | `TestAgentsCannotDecideChangeSets` | `internal/storage/integration_test.go` |
| R13 | `TestNotesOnSealedNodes` | `internal/storage/integration_test.go` |
| R04 | `TestCommitIsAtomic` | `internal/api/integration_test.go` |
| R05 | `TestProposedChangesInvisible` | `internal/api/integration_test.go` |
| R10 | `TestSealedCommitConflicts` | `internal/api/integration_test.go` |

Three existing tests change with this spec. `TestAppRoleCannotUpdateUnusedTables` (SPEC-002-R08) now checks the columns that are still not granted. In `TestSealedNodesAreImmutable` (SPEC-002-R05), the unsealed-sample update runs inside a committed change set. In `TestSigningChangesNothingElse` (SPEC-006-R06), the case where a scientist edits params runs inside a committed change set.

## 5. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Revisions are written by DB triggers, holding full snapshots | Can't be bypassed; easy to read back; diffs computed on demand | App-written history; storing diffs |
| 2026-10-18 | Every edit is a change set, even a direct one | One history model for humans, parsers and agents | Separate "edit" and "proposal" paths |
| 2026-10-30 | A revision's author and reason come from its change set's row. The trigger accepts only a change set committed in the current transaction (`xmin`). There is no `app.change_reason` setting | One source of truth for the reason. A stale or merely proposed change set can't authorise an edit | A separate reason setting, which could disagree with the change set |
| 2026-10-30 | SPEC-007 amends SPEC-002-R08. `lde_app` regains `UPDATE (label, attrs)` on `lineage_nodes` and `UPDATE (title)` on `experiments`, each behind `record_revise`. Every other column of those tables, and all of `instruments`, stays ungranted | The edits SPEC-002 blocked are now recorded and can't bypass the history, which is what 11.10(e) asks | Keeping R08 as is, which would make records uneditable |
| 2026-10-30 | Migration `0012` backfills revision 1 for existing rows, with no new audit events | Otherwise the first edit to an old record would become revision 1 and lose the original value. The creation events already exist | Starting history at the first edit |
| 2026-10-30 | `record.revised` is written for revisions after the first | A creation already has its own event | Two events for every creation |
| 2026-10-30 | `source`, `author_id` and `decided_by` are stamped by the database | A client can't claim to be human, or claim someone else's decision | Trusting the request |
| 2026-10-30 | Run params can be edited in any state until the run is sealed (signed or aborted). A sealed run's params raise `LDE04` | Open question 1. It also closes a gap for aborted runs | Editable only while planned |
| 2026-10-30 | `scientist` is enough to commit someone else's unsealed change set | Open question 2. Sealed records need SPEC-008's review anyway | Requiring `reviewer` |
| 2026-10-30 | Notes get `annotation` from the database (sealed at insert) | The client can't mislabel a note | A client flag |
| 2026-10-30 | Added `PATCH /v1/experiments/{id}`, `GET /v1/lineage/nodes/{id}/notes` and the `change_set.updated` audit action | The UI needs them, and every mutation is audited | |
| 2026-10-30 | `record_revise()` qualifies every relation as `public.…` and sets `search_path = pg_catalog, public, pg_temp`. Found by integrity review during task 1.6 | With `pg_temp` implicit and first, `lde_app` could shadow `change_sets` and `record_revisions` with temp tables and forge a committing change set | The design's original `search_path = pg_catalog, public` alone |
