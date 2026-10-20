# SPEC-013: Parser workers and ingest review

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(a), 11.10(e), 11.10(h) |
| **Depends on** | `SPEC-006`, `SPEC-007`, `SPEC-009`, `SPEC-012` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

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

## 3. Requirements (EARS)

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

## 4. Acceptance examples

Fixture: samples `S-001` and `S-002` exist. Raw file `vicell.csv` has rows `S-001` (viability 95.2) and `s 2` (viability 88.0). A parse job exists for it, and worker `wk1` (service) holds the lease.

**SPEC-013-R04**: `wk1` submits an `ingest_dataset` that references a different raw file → `422`, and the job stays claimed.

**SPEC-013-R05**: `wk1` submits matches `S-001` (1.0) and `s 2 → S-002` (0.8). Alice confirms only the first; her commit is rejected. After she confirms the second, the commit succeeds.

**SPEC-013-R06**: after that commit there is one dataset node `wasDerivedFrom vicell.csv`, edges `S-001 →used→ dataset` and `S-002 →used→ dataset`, and attrs valid against `lde/cell-count-result@1.0`.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Can we get a synthetic Vi-CELL BLU export (no real sample IDs) to use as the fixture and confirm the column layout? | Rob Bassett | |
| 2 | Should confirming a match need `reviewer`, or is the committing `scientist` enough? Recommendation: scientist. | Rob Bassett | |
| 3 | Is Python acceptable for reference workers on lab PCs, or should they be Go binaries? Recommendation: Python stdlib, the same as `tools/verify_audit.py`. | Rob Bassett | |
