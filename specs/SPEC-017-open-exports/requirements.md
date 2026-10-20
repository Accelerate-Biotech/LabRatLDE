# SPEC-017: Open exports

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(b) (accurate and complete copies), 11.10(c); EU Annex 11 §8 (printouts), §17 (archiving) |
| **Depends on** | `SPEC-003`, `SPEC-007`, `SPEC-008`, `SPEC-012` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

LabRat promises "no exit fee": a lab must be able to take everything out in formats that other tools read and that outlive LabRat. Part 11 also requires accurate and complete copies of records for inspection. Today only the audit chain can be exported. Lineage, readings, protocols, signatures and raw files can't leave except through `pg_dump`, which needs LabRat's schema to understand.

## 2. Scope

**In scope**
- **Lineage** as PROV-O JSON-LD.
- **Readings** as Parquet and CSV.
- **Protocol versions** as a ZIP of Markdown + YAML files.
- A **full tenant archive** (`labrat export`): a directory with every record type in open formats, raw file originals, and a `MANIFEST.json` listing every file's SHA-256 and size.
- A stdlib-only Python **archive verifier** (`tools/verify_archive.py`).

**Out of scope**
- Importing an archive into LabRat.
- Scheduled exports, and pushing exports to external storage.
- PDF "printouts" of records (open question 2).
- Formats beyond those listed (e.g. AnIML, Allotrope ADF).

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-017-R01 | When lineage is exported, the export API shall emit JSON-LD in which every node is a `prov:Entity`, `prov:Activity` (runs and analyses) or `prov:Agent` (instruments), and every edge is the PROV property of the same name. | no | sovereignty |
| SPEC-017-R02 | When readings are exported, the export API shall emit every reading of the dataset with timestamp (UTC, microseconds), channel, value and unit, in Parquet or CSV as requested. | yes | 11.10(b) |
| SPEC-017-R03 | When protocol versions are exported, the export API shall emit one `v<N>.md` file per version holding its exact stored text, plus a `versions.json` listing each version's SHA-256. | no | 11.10(b) |
| SPEC-017-R04 | When `labrat export --tenant <id> --out <dir>` runs, it shall write the audit chain (NDJSON), lineage (JSON-LD), every dataset's readings (Parquet), protocol versions, schema versions, revisions, change sets, signatures with their manifests (NDJSON), and every raw file original. | no | 11.10(b), Annex 11 §17 |
| SPEC-017-R05 | The tenant archive shall contain `MANIFEST.json` listing every other file's path, size and SHA-256, plus the audit chain head (seq and hash) at export time. | yes | 11.10(c) |
| SPEC-017-R06 | When `tools/verify_archive.py <dir>` runs, it shall verify every manifest hash, verify the audit chain with the v1 digest, check that raw file contents match their recorded SHA-256, and exit non-zero on any mismatch, missing file or extra file. | yes | 11.10(c) |
| SPEC-017-R07 | When a tenant archive is created, the export shall append an audit event `export.created` with the operator, the manifest SHA-256 and the chain head. | no | 11.10(e) |
| SPEC-017-R08 | The export API shall stream large exports without loading a whole dataset into memory. | no | robustness |

## 4. Acceptance examples

**SPEC-017-R02**: dataset `D` has 3 readings (`2026-10-20T10:00:00.000001Z, uv_254, 0.5, mAU`, …). The CSV has a header plus 3 rows, with timestamps byte-for-byte as shown. The Parquet file read with `pyarrow` gives the same 3 rows.

**SPEC-017-R05 / R06**: an archive of a tenant with 2 runs, 1 raw file and 40 audit events verifies. Each of these makes the verifier exit non-zero naming the problem: changing one byte of a Parquet file, deleting the raw file, adding an unlisted file, or editing an audit event's actor.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should archives be compressed (tar.zst) or left as a plain directory? Recommendation: a plain directory; compression is the operator's choice. | Rob Bassett | |
| 2 | Are human-readable PDF record reports (run + signatures) needed for inspections (Annex 11 §8)? Recommendation: a separate spec. | Rob Bassett | |
