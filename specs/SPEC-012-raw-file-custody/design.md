# SPEC-012 design: Raw file custody

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: files are written to `LDE_BLOB_DIR/<tenant>/<sha[0:2]>/<sha>`. The server writes to a temp file while hashing, fsyncs, renames, then makes the file read-only. An existing path means the bytes are already stored. Metadata lives in a `raw_files` table with an append-only trigger. If a transaction fails, an orphaned blob is harmless; garbage collection is out of scope.
- Not doing: a blob service, chunking, encryption at rest (left to the filesystem or volume), or a `storage` interface with one implementation.

## 2. Data and interfaces

- `raw_files(id, tenant_id, node_id, sha256 bytea, size bigint, original_filename, media_type, source ∈ {upload, stream, dropfolder}, uploaded_by, received_at)`. Append-only.
- New lineage kind `raw_file` (a new migration replaces the `kind` CHECK constraint).
- Config: `LDE_BLOB_DIR` (required in prod; `./data/blobs` in dev) and `LDE_MAX_RAW_BYTES`. SPEC-004 `labrat iq` gains a check that the blob directory is writable.
- API: `POST /v1/raw-files?instrument_id=&run_id=` (body is the file; headers `Content-Type`, `Content-Disposition` with filename), `GET /v1/raw-files/{id}`, `GET /v1/raw-files/{id}/content`, `POST /v1/raw-files/{id}/fixity`.
- Linking to a sealed run follows SPEC-002 and SPEC-008: rejected unless it goes through a correction.

## 3. UI

- Drop files onto a run view or the explorer to upload. A progress bar shows, and the resulting hash is displayed.
- Raw file view: metadata, hash, fixity status with a "re-check" button, download, and preview (text/CSV first 1 MB as a table; PDF and images inline; other types download only).

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Keep vendor originals, content-addressed (ROADMAP D9) | Part 11 original records; integrity by construction | Discarding originals after parsing |
| 2026-10-20 | Filesystem first, no storage abstraction | One real backend; S3 gets its own spec and decision when needed | An interface with one implementation |
