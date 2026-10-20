# SPEC-017 design: Open exports

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach:
  - JSON-LD is written with `encoding/json` from the lineage query; LabRat's relation names already are PROV terms.
  - CSV and ZIP use the standard library.
  - Parquet uses one dependency.
  - The archive is a plain directory built by a CLI subcommand running as the owner with `WithTenant` for the chosen tenant, so RLS still applies.
  - The verifier reuses the audit verifier's approach: stdlib only, no shared code.
- Not doing: a generic export framework, async export jobs in the API, or a custom container format.

## 2. Data and interfaces

- API: `GET /v1/lineage/{id}/export.jsonld?direction=`, `GET /v1/datasets/{id}/readings.parquet`, `GET /v1/datasets/{id}/readings.csv`, `GET /v1/experiments/{id}/protocol-versions.zip`.
- Archive layout: `MANIFEST.json`, `audit/chain.ndjson`, `lineage/graph.jsonld`, `datasets/<id>.parquet`, `protocols/<experiment-slug>/v<N>.md`, `schemas/<ns>/<name>/<ver>.json`, `revisions.ndjson`, `change_sets.ndjson`, `signatures.ndjson`, `raw/<sha256>`.
- JSON-LD `@context`: PROV-O plus `lde:` for LabRat terms. Attrs are emitted under `lde:attrs`, with the schema's `x-lde-ontology` as `@type` when present.

## 3. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Dependency `github.com/parquet-go/parquet-go` | Pure Go, no CGO (keeps the static binary) | `apache/arrow-go` (much larger) |
| 2026-10-20 | The archive verifier is stdlib Python and shares no code with the server | The same independence argument as `tools/verify_audit.py` | Verifying with the server itself |
