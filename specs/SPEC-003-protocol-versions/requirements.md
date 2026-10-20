# SPEC-003: Immutable protocol versions

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | 21 CFR 11.10(e), 11.10(k)(2) |
| **Depends on** | `SPEC-001`, `SPEC-002` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

A protocol (Markdown + YAML frontmatter) is stored once on its experiment, and a run points only at the experiment. Nothing records *which text* a run actually followed. If the protocol is ever revised, every earlier run silently appears to have followed the new text. Scientists lose reproducibility, and QA can't show which procedure was in force for a given run.

## 2. Scope

**In scope**
- Append-only, gaplessly numbered protocol versions per experiment, each with a database-computed SHA-256.
- Each version is a `protocol` lineage node, linked to the runs that followed it.
- Runs pin a protocol version from their own experiment.
- An API to add, list and read versions. Creating an experiment creates version 1.
- A backfill: every existing experiment gets version 1 from its current text, and every existing run is pinned to it.

**Out of scope**
- Editing an existing version (never possible).
- Approving or signing protocol versions (a later spec could reuse SPEC-008 signatures).
- Protocol diffs in the API. The UI diffs two version texts client-side.
- Validating run params against a protocol's params schema (SPEC-009).

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-003-R01 | The database shall compute each protocol version's `sha256` from the UTF-8 bytes of its `protocol_md`, and no caller shall be able to supply it. | yes | 11.10(e) |
| SPEC-003-R02 | If any role attempts to update or delete a protocol version, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.10(e) |
| SPEC-003-R03 | If a protocol version is inserted whose number is not the experiment's highest version + 1 (or 1 for the first), then the database shall reject it. | yes | 11.10(f) |
| SPEC-003-R04 | If a run references a protocol version that belongs to a different experiment, then the database shall reject it. | yes | data integrity |
| SPEC-003-R05 | When a run is created without a `protocol_version_id`, the run API shall pin the experiment's highest protocol version. | no | usability |
| SPEC-003-R06 | When a run is created, the run API shall link the protocol version's lineage node to the run node with a `used` edge in the same transaction. | yes | lineage correctness |
| SPEC-003-R07 | When a run is created, the audit event `run.created` shall include `protocol_version_id`, the version number and `protocol_sha256`. | yes | 11.10(e) |
| SPEC-003-R08 | When a protocol version is added, the API shall append an audit event `protocol.version_created` with the version number and `sha256`. | no | 11.10(e) |
| SPEC-003-R09 | When the migration runs, the database shall create version 1 for each existing experiment from its current `protocol_md` and pin every existing run to it. | no | migration |
| SPEC-003-R10 | When an experiment is read, the API shall return the text and metadata of its highest protocol version together with its version number. | no | usability |

## 4. Acceptance examples

**SPEC-003-R01**: inserting a version with `protocol_md` = `"# A\n"` yields `sha256` = SHA-256 of bytes `23 20 41 0a`. An insert that names `sha256` explicitly fails.

**SPEC-003-R02**: given version 1 of experiment `E`, `UPDATE protocol_versions SET protocol_md = 'x'` and `DELETE` each fail with `LDE03`, including as the table owner.

**SPEC-003-R03**: given `E` with versions 1 and 2, inserting version 4 fails and inserting version 3 succeeds. Inserting version 1 for a new experiment `F` succeeds.

**SPEC-003-R04**: given experiments `E` (v1 = `PE1`) and `F` (v1 = `PF1`), creating a run on `E` with `protocol_version_id = PF1` fails.

**SPEC-003-R06**: creating a run on `E` pinned to `PE1` yields an edge `PE1-node` →used→ `run-node`. The run's upstream lineage contains `PE1-node`.

**SPEC-003-R07**: that run's `run.created` payload contains `"protocol_version": 1` and a `protocol_sha256` equal to `PE1.sha256` in lowercase hex.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Drop `experiments.protocol_md` and `protocol_meta` in a later migration, or keep them frozen as v1 copies? Recommendation: drop once nothing reads them. | Rob Bassett | |
