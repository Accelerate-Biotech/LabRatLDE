# SPEC-007 design: Revisions, change sets and notes

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one generic `record_revisions` table, filled by `AFTER INSERT/UPDATE` triggers on the revisable tables. The triggers read `app.change_set_id` and `app.change_reason`, which the API sets for the transaction. `change_sets` holds proposed items as JSON. Commit replays the items through the same code paths as direct edits. Diff and blame are pure Go in `internal/domain`.
- Not doing: per-table history tables, an event-sourcing framework, three-way merge, or storing diffs instead of snapshots.

## 2. Data and interfaces

- `record_revisions(tenant_id, entity_type, entity_id, rev, value jsonb, author, reason, change_set_id, created_at)`, PK `(tenant_id, entity_type, entity_id, rev)`, append-only.
- `change_sets(id, tenant_id, title, reason, source ∈ {human, parser, agent}, author, state, items jsonb, items_sha256, decided_by, decided_at, decision_reason, created_at)`.
- `notes(id, tenant_id, node_id, body, annotation bool, created_by, created_at)`. `body` is revisable.
- Item ops in v1: `update_node{node_id, label?, attrs?}`, `update_run_params{run_id, params}`, `set_experiment_title`, `add_node`, `add_edge`, `add_note`, `edit_note`. SPEC-013 adds `ingest_dataset`.
- Column-level grants: `UPDATE (label, attrs) ON lineage_nodes`, `UPDATE (title) ON experiments`, `UPDATE (body) ON notes` to `lde_app`. This re-grants what SPEC-002 revoked, now covered by revision triggers.
- API: `POST /v1/change-sets`, `GET /v1/change-sets?state=&source=`, `GET/PUT /v1/change-sets/{id}`, `POST /v1/change-sets/{id}/{commit|reject|withdraw}` (reason required), `PATCH /v1/lineage/nodes/{id}`, `PATCH /v1/runs/{id}/params`, `POST /v1/lineage/nodes/{id}/notes`, `PATCH /v1/notes/{id}`, `GET /v1/records/{type}/{id}/revisions[/{rev}]`, `GET …/diff?from=&to=`, `GET …/blame`.
- Audit actions: `change_set.proposed`, `change_set.committed` (with `items_sha256`), `change_set.rejected`, `change_set.withdrawn`, `record.revised` (one per revision), `note.created`.

## 3. UI

- Every editable field has an edit control. Saving asks for a reason (required), and the field shows a "changed" marker linking to its history.
- History panel: a revision list for the open record, a side-by-side diff of any two revisions, and per-field blame on hover.
- Notes: a Markdown editor under the run view. Annotations on sealed records are visibly labelled.
- Review inbox: proposed change sets, grouped by source, each showing the items as diffs, with commit or reject plus a reason.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Revisions are written by DB triggers, holding full snapshots | Can't be bypassed; easy to read back; diffs computed on demand | App-written history; storing diffs |
| 2026-10-18 | Every edit is a change set, even a direct one | One history model for humans, parsers and agents | Separate "edit" and "proposal" paths |
