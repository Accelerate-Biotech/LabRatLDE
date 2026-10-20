# SPEC-012: Raw file custody

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(b), 11.10(c), 11.10(e); EU Annex 11 §7 (data storage) |
| **Depends on** | `SPEC-002`, `SPEC-006`, `SPEC-007` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Most instruments produce files (CSV, XLSX, vendor exports, PDFs), not NDJSON streams. Part 11 expects the original record to be kept and retrievable in a human-readable form. Today LabRat stores only parsed numbers, and the exact bytes received are reduced to a hash in the audit trail. Parsers (SPEC-013) need somewhere trustworthy to read originals from. Auditors need to prove that the file in LabRat is bit-for-bit the file the instrument wrote.

## 2. Scope

**In scope**
- A content-addressed store for original files on the local filesystem, per tenant.
- A `raw_file` lineage node pinned to the file's SHA-256 and size.
- Upload, download and metadata APIs, with optional links to an instrument and a run.
- Fixity checks, on demand and with `labrat fixity`.
- The NDJSON stream body is also kept as a raw file, and its dataset is derived from it.
- UI: drag-and-drop upload, metadata view, download and preview.

**Out of scope**
- S3 or object storage backends (a later decision; ROADMAP D9).
- Deleting files and retention schedules (open question 1).
- Parsing files (SPEC-013).
- Deduplication across tenants.

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-012-R01 | When a file is uploaded, the raw file API shall compute the SHA-256 and size of the exact bytes received and store both, with original filename, media type, uploader and receive time (database clock). | yes | chain of custody |
| SPEC-012-R02 | The raw file API shall commit the metadata row only after the file's bytes are durably stored (fsync) at the content address. | no | 11.10(c) |
| SPEC-012-R03 | If any role attempts to update or delete raw file metadata, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.10(c) |
| SPEC-012-R04 | When a fixity check runs, the raw file API shall re-hash the stored bytes and report `ok` or `mismatch` (with expected and actual hash) for each file checked. | yes | 11.10(c), Annex 11 §7.1 |
| SPEC-012-R05 | When a file is downloaded, the raw file API shall return the stored bytes unchanged, with the original filename, media type, and an `X-LDE-SHA256` header. | yes | 11.10(b) |
| SPEC-012-R06 | When a raw file is received, the API shall create a `raw_file` lineage node, link it `wasAttributedTo` the named instrument and `wasGeneratedBy` the named run when given, and append an audit event `raw_file.received` with SHA-256, size, filename and media type. | no | lineage |
| SPEC-012-R07 | When an NDJSON stream is ingested, the ingest API shall also store its exact bytes as a raw file and link the dataset `wasDerivedFrom` it. | no | chain of custody |
| SPEC-012-R08 | If an upload exceeds `LDE_MAX_RAW_BYTES` (default 2 GiB), then the raw file API shall stop reading, store nothing, and respond `413`. | no | resource safety |
| SPEC-012-R09 | When `labrat fixity [--tenant <id>]` runs, it shall check every stored file and exit non-zero if any mismatch or missing file is found. | no | operations |

## 4. Acceptance examples

**SPEC-012-R01**: uploading the 11-byte body `hello world` as `cells.csv` (`text/csv`) gives `sha256 = b94d27b9…efcde9`, `size = 11`, `original_filename = "cells.csv"`.

**SPEC-012-R03**: `UPDATE raw_files SET original_filename = 'x'` and `DELETE`, run as the owner, each fail with `LDE03`.

**SPEC-012-R04**: after the upload above, a fixity check reports `ok`. After one byte of the stored blob is changed outside LabRat, it reports `mismatch` with expected `b94d…` and the actual hash. After the blob is removed, it reports `missing`.

**SPEC-012-R05**: downloading that file returns exactly `hello world`, `Content-Disposition: attachment; filename="cells.csv"` and `X-LDE-SHA256: b94d…`.

## 5. Non-functional

- Upload throughput shall be at least 100 MB/s to local SSD on a 4-core server.

## 6. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Retention: must originals be kept forever, or for a configured period with an audited, QA-approved disposal? Recommendation: keep forever in v1; disposal needs its own spec. | Rob Bassett | |
| 2 | Is a backup procedure for `LDE_BLOB_DIR` in scope for the documentation (it must be backed up with the database)? | Rob Bassett | |
