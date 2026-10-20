# SPEC-007: Revisions, change sets and notes

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | 21 CFR 11.10(e) ("shall not obscure previously recorded information"), 11.10(c) |
| **Depends on** | `SPEC-002`, `SPEC-005`, `SPEC-006` |

## 1. Problem

Scientists routinely correct records (a mistyped volume), add to them (a late observation) and annotate them (a corrected note). Today LabRat can't edit anything after creation. And if it could, nothing would keep the earlier value, which Part 11 requires. Parsers and agents (SPEC-013, SPEC-014) also need a way to *propose* changes that a human accepts. Git solves both with history, diffs, commits and pull requests. LabRat needs the same mechanics for lab records, without any way to rewrite history.

## 2. Scope

**In scope**
- **Revisions:** an append-only history for each revisable record, written by database triggers so nothing can bypass it.
- **Revisable fields in v1:** lineage node `label` and `attrs`, run `params`, experiment `title`, note `body`.
- **Change sets:** a group of edits with a reason, moving `proposed → committed | rejected | withdrawn`, applied atomically.
- **Direct edits** to unsealed records, recorded as a one-item change set committed in the same request.
- **Notes:** revisioned Markdown attached to a lineage node.
- **History APIs:** log, read one revision, field-level diff, blame.
- **UI:** history/diff panel, notes editor, edit-with-reason, review inbox.

**Out of scope**
- Committing change sets that touch **sealed** records. R10 rejects them until SPEC-008, which defines review, signature invalidation and re-signing.
- Revisions for audit events, signatures, protocol versions or readings (those are immutable).
- Branching or merging lab data.
- Collaborative real-time editing.

## 3. Simplicity budget

- Approach: one generic `record_revisions` table, filled by `AFTER INSERT/UPDATE` triggers on the revisable tables. The triggers read `app.change_set_id` and `app.change_reason`, which the API sets for the transaction. `change_sets` holds proposed items as JSON. Commit replays the items through the same code paths as direct edits. Diff and blame are pure Go in `internal/domain`.
- Not doing: per-table history tables, an event-sourcing framework, three-way merge, or storing diffs instead of snapshots.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-007-R01 | When a revisable record is inserted or a revisable field changes, the database shall append a revision holding the full new value, the revision number (previous + 1), author, reason, change set id and time, in the same transaction. | yes | 11.10(e) |
| SPEC-007-R02 | If a revisable field changes in a transaction without a committing change set and a non-blank reason, then the database shall reject the statement. | yes | 11.10(e) |
| SPEC-007-R03 | If any role attempts to update, delete or truncate revisions, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.10(c), (e) |
| SPEC-007-R04 | When a change set is committed, the change set API shall apply all of its items, revisions and audit events in one transaction, or none of them. | yes | data integrity |
| SPEC-007-R05 | While a change set is not committed, its items shall not appear in record reads, lineage closures, queries or signature manifests. | yes | data integrity |
| SPEC-007-R06 | If a principal of kind `agent` or `service` commits or rejects a change set, then the database shall reject it with SQLSTATE `LDE05`. | yes | 11.10(g), ROADMAP D6 |
| SPEC-007-R07 | The revision API shall return a record's revisions oldest first, and any single revision's full value. | no | user need |
| SPEC-007-R08 | The revision API shall return a field-level diff (added, removed, changed, with JSON paths) between any two revisions of a record. | no | user need |
| SPEC-007-R09 | The revision API shall return, for each leaf field of a record's latest revision, the revision number, author and time that last changed it. | no | user need |
| SPEC-007-R10 | If a change set touches a sealed record (SPEC-002), then the change set API shall reject its commit with `409` until SPEC-008 defines the correction flow. | yes | 11.10(c) |
| SPEC-007-R11 | When a direct edit is made to an unsealed record, the API shall record it as a single-item change set authored and committed by the editor in the same transaction. | no | uniform history |
| SPEC-007-R12 | While a change set is `proposed`, only its author shall be able to replace its items or withdraw it; `committed`, `rejected` and `withdrawn` shall be terminal. | no | workflow |
| SPEC-007-R13 | When a note is added to a sealed node, the notes API shall accept it as an annotation (`annotation = true`) without breaking the seal; editing a sealed node's existing non-annotation note shall count as touching a sealed record. | yes | 11.10(e) |

## 5. Acceptance examples

Fixture: alice (scientist), bob (reviewer), bot (agent acting for alice). Sample node `S`, label "Plasma 042", attrs `{"volume_ul": 50}`.

**SPEC-007-R01**: alice edits `S` attrs to `{"volume_ul": 45}` with reason "pipetting error". `S` now has revisions 1 (`50`, author alice, reason "created") and 2 (`45`, reason "pipetting error"), and the current row shows `45`. Revision 1 is still readable.

**SPEC-007-R02**: in a transaction that sets no change set, `UPDATE lineage_nodes SET attrs = '{}' WHERE id = S` fails. With a change set but a reason of `"  "`, it also fails.

**SPEC-007-R03**: `UPDATE`, `DELETE` and `TRUNCATE` on `record_revisions`, run as the owner, each fail with `LDE03`.

**SPEC-007-R04**: a change set with items [edit `S` attrs, add edge `S` →used→ `R`, add edge `R` →used→ `S`] is committed. The second edge creates a cycle, so nothing is applied: `S` stays at its previous revision and the audit chain length is unchanged.

**SPEC-007-R05**: bot proposes adding node `N` and editing `S` label to "P-042". Before commit, `GET S` shows "Plasma 042", `N` doesn't exist, and upstream lineage of `R` excludes `N`. After alice commits, both appear.

**SPEC-007-R06**: with bot acting for alice, bot's commit of its own change set fails with `LDE05`.

**SPEC-007-R10**: run `R` is signed (SPEC-002 seal). A change set editing `R` params is created successfully, but its commit returns `409`.

**SPEC-007-R13**: alice adds note "Freezer alarm at 02:10, samples unaffected" to sealed run `R`. It succeeds with `annotation = true` and `R` remains sealed. A change set editing a note written before signing is rejected at commit, as in R10.

## 6. Data and interfaces

- `record_revisions(tenant_id, entity_type, entity_id, rev, value jsonb, author, reason, change_set_id, created_at)`, PK `(tenant_id, entity_type, entity_id, rev)`, append-only.
- `change_sets(id, tenant_id, title, reason, source ∈ {human, parser, agent}, author, state, items jsonb, items_sha256, decided_by, decided_at, decision_reason, created_at)`.
- `notes(id, tenant_id, node_id, body, annotation bool, created_by, created_at)`. `body` is revisable.
- Item ops in v1: `update_node{node_id, label?, attrs?}`, `update_run_params{run_id, params}`, `set_experiment_title`, `add_node`, `add_edge`, `add_note`, `edit_note`. SPEC-013 adds `ingest_dataset`.
- Column-level grants: `UPDATE (label, attrs) ON lineage_nodes`, `UPDATE (title) ON experiments`, `UPDATE (body) ON notes` to `lde_app`. This re-grants what SPEC-002 revoked, now covered by revision triggers.
- API: `POST /v1/change-sets`, `GET /v1/change-sets?state=&source=`, `GET/PUT /v1/change-sets/{id}`, `POST /v1/change-sets/{id}/{commit|reject|withdraw}` (reason required), `PATCH /v1/lineage/nodes/{id}`, `PATCH /v1/runs/{id}/params`, `POST /v1/lineage/nodes/{id}/notes`, `PATCH /v1/notes/{id}`, `GET /v1/records/{type}/{id}/revisions[/{rev}]`, `GET …/diff?from=&to=`, `GET …/blame`.
- Audit actions: `change_set.proposed`, `change_set.committed` (with `items_sha256`), `change_set.rejected`, `change_set.withdrawn`, `record.revised` (one per revision), `note.created`.

## 7. UI notes

- Every editable field has an edit control. Saving asks for a reason (required), and the field shows a "changed" marker linking to its history.
- History panel: a revision list for the open record, a side-by-side diff of any two revisions, and per-field blame on hover.
- Notes: a Markdown editor under the run view. Annotations on sealed records are visibly labelled.
- Review inbox: proposed change sets, grouped by source, each showing the items as diffs, with commit or reject plus a reason.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should run params stay editable while a run is `running`, or only while `planned`? Recommendation: editable until the first signature, always with a reason. | Rob Bassett | |
| 2 | Should committing someone else's proposed change set (not sealed) need `reviewer`, or is `scientist` enough? Recommendation: `scientist`, since sealed records already need review (SPEC-008). | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Revisions are written by DB triggers, holding full snapshots | Can't be bypassed; easy to read back; diffs computed on demand | App-written history; storing diffs |
| 2026-10-18 | Every edit is a change set, even a direct one | One history model for humans, parsers and agents | Separate "edit" and "proposal" paths |
