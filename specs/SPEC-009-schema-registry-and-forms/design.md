# SPEC-009 design: Schema registry and schema-driven forms

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: tables `schemas` and `schema_versions`, with a generated SHA-256 over the stored text. Nodes and runs get a `schema_version_id` column, guarded by a trigger requiring published status and a matching kind. Validation uses one Go library, wrapped in a single function in `internal/domain`. Forms use one React library driven directly by the schema, plus a few `x-lde-*` keywords.
- Not doing: a custom schema language, a form designer, or validation inside PostgreSQL.

## 2. Data and interfaces

- `schemas(id, tenant_id, namespace, name, applies_to, created_by, created_at)`, with `UNIQUE (tenant_id, namespace, name)`.
- `schema_versions(id, tenant_id, schema_id, major, minor, status ∈ {draft, published}, body_text, body jsonb, sha256 GENERATED, author, published_by, published_at)`.
- `lineage_nodes.schema_version_id` and `runs.params_schema_version_id`: nullable, with the R03 trigger. Protocol frontmatter key `params_schema: <ns>/<name>@<major>.<minor>`.
- New SQLSTATE `LDE06` = invalid schema reference, mapped to `422`.
- API: `GET/POST /v1/schemas`, `GET /v1/schemas/{ns}/{name}`, `POST …/versions` (draft), `PUT …/versions/{ver}` (draft text), `POST …/versions/{ver}/publish`, `GET …/versions/{ver}`, `POST /v1/schemas/validate` (dry run: schema version + instance → errors).
- UI keywords: `x-lde-unit`, `x-lde-widget` (`textarea`, `date`, `node-picker:<kind>`), `x-lde-order`. Unknown `x-` keywords are ignored by validation.
- Audit actions: `schema.created`, `schema.version_drafted`, `schema.version_published`.

## 3. UI

- Forms appear wherever node attrs or run params are created or edited (SPEC-007 edit-with-reason). Validation errors also go to the Problems panel.
- Schema browser: namespaces → schemas → versions, with rendered field docs and a sample form preview.
- Draft editor: JSON text with live preview of the generated form and validation messages.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | JSON Schema 2020-12 is the stored format (ROADMAP D3) | Mature Go and JS validators; LinkML can compile to it | LinkML as stored format (Python-only tooling) |
| 2026-10-20 | Dependency `github.com/santhosh-tekuri/jsonschema/v6` | Full 2020-12 support, custom loaders (registry-only `$ref`), pure Go | `xeipuuv/gojsonschema` (draft-07 only) |
| 2026-10-20 | Conformance checked in Go; the DB enforces reference validity (published + kind) | No stock PG extension validates JSON Schema; the DB still guards the "never" rules | `pg_jsonschema` (extension not available in typical managed PG) |
