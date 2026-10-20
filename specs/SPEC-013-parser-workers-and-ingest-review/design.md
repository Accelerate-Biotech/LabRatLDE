# SPEC-013 design: Parser workers and ingest review

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one `parse_jobs` table with a lease, and four endpoints for workers. Worker output is an ordinary SPEC-007 change set with `source = parser`. Match resolutions are stored in the change set's items and validated at commit. The watcher and parser live in `workers/vicell/` and talk to LabRat only through the public API.
- Not doing: a message queue, a plugin system loaded into the server, or a parser SDK beyond a documented JSON contract.

## 2. Data and interfaces

- `parse_jobs(id, tenant_id, raw_file_id, parser, state ∈ {queued, claimed, submitted, failed}, worker_id, lease_until, change_set_id, error, created_at)`.
- Worker API: `GET /v1/parse-jobs?state=queued&parser=`, `POST /v1/parse-jobs/{id}/claim`, `POST /v1/parse-jobs/{id}/result {items, parser_version, warnings}`, `POST /v1/parse-jobs/{id}/fail {error}`. `POST /v1/raw-files` gains `?parser=`.
- Change set op `ingest_dataset{raw_file_id, label, instrument_id?, run_id?, schema_version_id?, rows?:[{attrs, sample_match:{source_id, candidates:[{node_id, confidence}], resolution?:{node_id|create, by, at}}}], readings?:[…]}`.
- `PUT /v1/change-sets/{id}/matches/{row}` (human only) records a resolution.
- Starter schema `lde/cell-count-result@1.0` (needs SPEC-009 open question 2).
- Worker config (`workers/vicell/config.toml`, read with Python `tomllib`): LabRat URL, client credentials, and a folder → {instrument, parser} map.

## 3. UI

- Ingest review queue: proposed parser change sets, newest first, showing the instrument, file name and the count of unresolved matches.
- Review screen: raw file preview (left), parsed rows (right), and per-row match chips with confidence. Low confidence is highlighted. Actions are confirm, choose another sample (node picker) or create a new sample. Schema problems appear in the Problems panel. Commit is enabled only when all matches are resolved and no blocking problems remain.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Parsers run out of process and submit change sets (ROADMAP D9) | Parsers churn; the validated core stays small; any language works | In-binary parser plugins |
| 2026-10-20 | No auto-commit at any confidence (ROADMAP D10) | Human accountability for the record | A confidence threshold for auto-accept |
