# SPEC-009: Schema registry and schema-driven forms

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(h) (input checks), 11.10(e) |
| **Depends on** | `SPEC-003`, `SPEC-005`, `SPEC-006`, `SPEC-007` |

## 1. Problem

Node `attrs` and run `params` are free JSON. Two scientists record the same measurement as `volume_ul` and `Volume (µL)`, typos go unnoticed, and nothing can be compared across experiments. Labs need structured, versioned definitions of what a sample, reagent lot or run's parameters must contain. Scientists shouldn't have to write JSON, so the same definition should give them a form.

## 2. Scope

**In scope**
- A registry of JSON Schema 2020-12 documents, identified as `namespace/name@major.minor`.
- Versions are `draft` (editable by their author) or `published` (immutable).
- Each schema declares what it applies to: a lineage node kind, or `run_params`.
- Nodes may reference a published schema version; protocol frontmatter may name a `params_schema`.
- Writes are validated against the referenced schema, and validation errors are reported per field.
- `$ref` is resolved only inside the registry. Nothing is fetched remotely.
- UI: forms rendered from schemas, a schema browser and a draft editor.

**Out of scope**
- Who may publish where, and compatibility rules. In this spec any `scientist` may publish; SPEC-010 replaces that with per-namespace governance.
- Making a schema mandatory for a node kind (SPEC-010, Controlled namespaces).
- LinkML import, and migrating existing data between schema versions.

## 3. Simplicity budget

- Approach: tables `schemas` and `schema_versions`, with a generated SHA-256 over the stored text. Nodes and runs get a `schema_version_id` column, guarded by a trigger requiring published status and a matching kind. Validation uses one Go library, wrapped in a single function in `internal/domain`. Forms use one React library driven directly by the schema, plus a few `x-lde-*` keywords.
- Not doing: a custom schema language, a form designer, or validation inside PostgreSQL.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-009-R01 | If any role attempts to update or delete a published schema version, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.10(e) |
| SPEC-009-R02 | The database shall compute each schema version's `sha256` from the UTF-8 bytes of its stored text. | no | traceability |
| SPEC-009-R03 | If a lineage node references a schema version that is not published, or whose `applies_to` differs from the node's kind, then the database shall reject it with SQLSTATE `LDE06`. | yes | data integrity |
| SPEC-009-R04 | If a protocol version's `params_schema` names a schema version that is not published or does not apply to `run_params`, then the API shall reject the protocol version with `422`. | no | data integrity |
| SPEC-009-R05 | If a node's attrs, or a run's params, do not validate against the referenced schema version, then the API shall reject the write with `422` and list each error's JSON pointer and message. | yes | 11.10(h) |
| SPEC-009-R06 | When a schema-backed record is created or revised, the audit event shall include the schema version id and its `sha256`. | no | 11.10(e) |
| SPEC-009-R07 | If a schema contains a `$ref` that doesn't resolve to a published registry version (`lde://schemas/<ns>/<name>/<major>.<minor>`) or a local fragment, then the registry shall refuse to publish it. | no | air-gap, reproducibility |
| SPEC-009-R08 | If a schema is not valid JSON Schema 2020-12, then the registry shall refuse to save it as a draft. | no | usability |
| SPEC-009-R09 | When a record is opened for editing, the UI shall render a form from its schema version, showing titles, descriptions, units (`x-lde-unit`) and inline validation errors. | no | user need |
| SPEC-009-R10 | While a schema version is a draft, only its author shall be able to change its text. | no | workflow |

## 5. Acceptance examples

Fixture: schema `lab/sample@1.0`, applying to `sample`:

```json
{"type":"object","required":["volume_ul"],"properties":{"volume_ul":{"type":"number","minimum":0,"x-lde-unit":"µL"},"matrix":{"enum":["plasma","serum"]}}}
```

**SPEC-009-R01**: after publish, `UPDATE schema_versions SET body = '{}'` fails with `LDE03`, even as the owner. A draft `lab/sample@1.1` can still be edited by its author.

**SPEC-009-R03**: creating a `sample` node that references draft `lab/sample@1.1` fails with `LDE06`. Creating a `reagent` node that references `lab/sample@1.0` fails with `LDE06`.

**SPEC-009-R05**: creating a sample with attrs `{"volume_ul": -5, "matrix": "urine"}` returns `422` with errors at `/volume_ul` (minimum) and `/matrix` (enum). `{"volume_ul": 50, "matrix": "plasma"}` succeeds. A run whose protocol version names `params_schema: lab/qpcr-params@1.0` and whose params lack required `cycles` returns `422`.

## 6. Data and interfaces

- `schemas(id, tenant_id, namespace, name, applies_to, created_by, created_at)`, with `UNIQUE (tenant_id, namespace, name)`.
- `schema_versions(id, tenant_id, schema_id, major, minor, status ∈ {draft, published}, body_text, body jsonb, sha256 GENERATED, author, published_by, published_at)`.
- `lineage_nodes.schema_version_id` and `runs.params_schema_version_id`: nullable, with the R03 trigger. Protocol frontmatter key `params_schema: <ns>/<name>@<major>.<minor>`.
- New SQLSTATE `LDE06` = invalid schema reference, mapped to `422`.
- API: `GET/POST /v1/schemas`, `GET /v1/schemas/{ns}/{name}`, `POST …/versions` (draft), `PUT …/versions/{ver}` (draft text), `POST …/versions/{ver}/publish`, `GET …/versions/{ver}`, `POST /v1/schemas/validate` (dry run: schema version + instance → errors).
- UI keywords: `x-lde-unit`, `x-lde-widget` (`textarea`, `date`, `node-picker:<kind>`), `x-lde-order`. Unknown `x-` keywords are ignored by validation.
- Audit actions: `schema.created`, `schema.version_drafted`, `schema.version_published`.

## 7. UI notes

- Forms appear wherever node attrs or run params are created or edited (SPEC-007 edit-with-reason). Validation errors also go to the Problems panel.
- Schema browser: namespaces → schemas → versions, with rendered field docs and a sample form preview.
- Draft editor: JSON text with live preview of the generated form and validation messages.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Form library: `@rjsf/core` (react-jsonschema-form; recommended, mature) or a small in-house renderer for a schema subset? | Rob Bassett | |
| 2 | Should LabRat ship starter schemas (sample, reagent lot, instrument status) in a system namespace `lde`? SPEC-015 assumes yes for inventory and equipment. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | JSON Schema 2020-12 is the stored format (ROADMAP D3) | Mature Go and JS validators; LinkML can compile to it | LinkML as stored format (Python-only tooling) |
| 2026-10-20 | Dependency `github.com/santhosh-tekuri/jsonschema/v6` | Full 2020-12 support, custom loaders (registry-only `$ref`), pure Go | `xeipuuv/gojsonschema` (draft-07 only) |
| 2026-10-20 | Conformance checked in Go; the DB enforces reference validity (published + kind) | No stock PG extension validates JSON Schema; the DB still guards the "never" rules | `pg_jsonschema` (extension not available in typical managed PG) |
