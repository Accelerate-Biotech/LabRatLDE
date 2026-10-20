# SPEC-013: Parser workers and ingest review

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(a), 11.10(e), 11.10(h) |
| **Depends on** | `SPEC-006`, `SPEC-007`, `SPEC-009`, `SPEC-012` |

## 1. Problem

Scientists retype instrument results (cell counts, viabilities, metabolite readings) from exported files into records. That is slow and error-prone. The design's "zero-entry ingestion" means machines read the files, match results to the right samples, and stage them for a human to accept. Parsers change with every instrument firmware, so they must stay outside the validated core. Fuzzy matching makes mistakes, so nothing it produces may enter the record without human confirmation.

## 2. Scope

**In scope**
- **Parse jobs:** created when a raw file arrives with a parser name. Out-of-process workers (service principals) claim them, fetch the file, and submit either a **change set** or a failure.
- A new change set op `ingest_dataset`, covering tabular per-sample results (schema-validated) and time-series readings.
- **Sample matching:** the worker proposes candidates with confidence scores, and a human must confirm, choose or create each match before commit.
- A **drop-folder watcher** as part of the worker distribution.
- One **reference parser** (Vi-CELL BLU CSV export), written in Python with only the standard library.
- **Ingest review UI**.

**Out of scope**
- Parsers for other instruments (each is its own small spec or contribution).
- Direct instrument control or LIMS/TetraScience connectors (later specs).
- Automatic commit, even at 100% confidence (ROADMAP D10).
- ML-based anomaly detection. v1 checks are schema validation plus parser-reported warnings.

## 3. Simplicity budget

- Approach: one `parse_jobs` table with a lease, and four endpoints for workers. Worker output is an ordinary SPEC-007 change set with `source = parser`. Match resolutions are stored in the change set's items and validated at commit. The watcher and parser live in `workers/vicell/` and talk to LabRat only through the public API.
- Not doing: a message queue, a plugin system loaded into the server, or a parser SDK beyond a documented JSON contract.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-013-R01 | When a raw file is received with a `parser` name, the ingest API shall create a parse job in state `queued` that references the raw file. | no | workflow |
| SPEC-013-R02 | When a worker claims a parse job, the ingest API shall lease it to that worker for `LDE_PARSE_LEASE` (default 10 min), and an expired lease shall return the job to `queued`. | no | robustness |
| SPEC-013-R03 | When a worker submits a result, the ingest API shall store it as a proposed change set with `source = parser` whose `ingest_dataset` item references the job's raw file. | no | workflow |
| SPEC-013-R04 | If an `ingest_dataset` item references a raw file other than its parse job's, or no raw file at all, then the ingest API shall reject the submission. | yes | chain of custody |
| SPEC-013-R05 | If a change set containing sample matches is committed while any match is not confirmed by a human principal, then the change set API shall reject the commit. | yes | ROADMAP D10, 11.10(h) |
| SPEC-013-R06 | When an `ingest_dataset` item is committed, the change set API shall create a dataset node `wasDerivedFrom` the raw file, `used`-linked from each confirmed sample, and validated against the item's schema version. | yes | lineage correctness |
| SPEC-013-R07 | When an `ingest_dataset` item is committed, the audit event shall record the raw file SHA-256, parser name and version, worker principal, row count, and each match's confidence and confirming human. | no | 11.10(e) |
| SPEC-013-R08 | If a submitted result fails schema validation, then the ingest API shall still store the change set, attaching each validation error as a blocking problem that prevents commit. | no | 11.10(h) |
| SPEC-013-R09 | When the drop-folder watcher sees a new, stable file (unchanged for 10 s) in a configured folder, it shall upload it with that folder's instrument and parser, and move it to `processed/` only after LabRat returns its SHA-256 and the watcher's own hash matches. | no | chain of custody |
| SPEC-013-R10 | The reference Vi-CELL BLU parser shall map each result row to the `lde/cell-count-result` schema and propose sample matches by exact ID, then by normalised ID (case, whitespace, leading zeros), reporting confidence 1.0 and 0.8 respectively. | no | user need |

## 5. Acceptance examples

Fixture: samples `S-001` and `S-002` exist. Raw file `vicell.csv` has rows `S-001` (viability 95.2) and `s 2` (viability 88.0). A parse job exists for it, and worker `wk1` (service) holds the lease.

**SPEC-013-R04**: `wk1` submits an `ingest_dataset` that references a different raw file → `422`, and the job stays claimed.

**SPEC-013-R05**: `wk1` submits matches `S-001` (1.0) and `s 2 → S-002` (0.8). Alice confirms only the first; her commit is rejected. After she confirms the second, the commit succeeds.

**SPEC-013-R06**: after that commit there is one dataset node `wasDerivedFrom vicell.csv`, edges `S-001 →used→ dataset` and `S-002 →used→ dataset`, and attrs valid against `lde/cell-count-result@1.0`.

## 6. Data and interfaces

- `parse_jobs(id, tenant_id, raw_file_id, parser, state ∈ {queued, claimed, submitted, failed}, worker_id, lease_until, change_set_id, error, created_at)`.
- Worker API: `GET /v1/parse-jobs?state=queued&parser=`, `POST /v1/parse-jobs/{id}/claim`, `POST /v1/parse-jobs/{id}/result {items, parser_version, warnings}`, `POST /v1/parse-jobs/{id}/fail {error}`. `POST /v1/raw-files` gains `?parser=`.
- Change set op `ingest_dataset{raw_file_id, label, instrument_id?, run_id?, schema_version_id?, rows?:[{attrs, sample_match:{source_id, candidates:[{node_id, confidence}], resolution?:{node_id|create, by, at}}}], readings?:[…]}`.
- `PUT /v1/change-sets/{id}/matches/{row}` (human only) records a resolution.
- Starter schema `lde/cell-count-result@1.0` (needs SPEC-009 open question 2).
- Worker config (`workers/vicell/config.toml`, read with Python `tomllib`): LabRat URL, client credentials, and a folder → {instrument, parser} map.

## 7. UI notes

- Ingest review queue: proposed parser change sets, newest first, showing the instrument, file name and the count of unresolved matches.
- Review screen: raw file preview (left), parsed rows (right), and per-row match chips with confidence. Low confidence is highlighted. Actions are confirm, choose another sample (node picker) or create a new sample. Schema problems appear in the Problems panel. Commit is enabled only when all matches are resolved and no blocking problems remain.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Can we get a synthetic Vi-CELL BLU export (no real sample IDs) to use as the fixture and confirm the column layout? | Rob Bassett | |
| 2 | Should confirming a match need `reviewer`, or is the committing `scientist` enough? Recommendation: scientist. | Rob Bassett | |
| 3 | Is Python acceptable for reference workers on lab PCs, or should they be Go binaries? Recommendation: Python stdlib, the same as `tools/verify_audit.py`. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Parsers run out of process and submit change sets (ROADMAP D9) | Parsers churn; the validated core stays small; any language works | In-binary parser plugins |
| 2026-10-20 | No auto-commit at any confidence (ROADMAP D10) | Human accountability for the record | A confidence threshold for auto-accept |
