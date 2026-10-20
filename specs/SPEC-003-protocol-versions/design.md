# SPEC-003 design: Immutable protocol versions

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one `protocol_versions` table with a generated `sha256` column, an append-only trigger, a gapless-version trigger, and a composite FK from `runs`. One new migration.
- Not doing: a separate protocol entity independent of experiments, branching of protocols inside LabRat (authors can branch in git and push), or PROV qualified associations.

## 2. Data and interfaces

New table `protocol_versions`:

| Column | Type | Notes |
|---|---|---|
| `id` | uuid | PK |
| `tenant_id` | uuid | RLS; `UNIQUE (tenant_id, experiment_id, id)` |
| `experiment_id` | uuid | composite FK to `experiments` |
| `version` | int | `UNIQUE (tenant_id, experiment_id, version)`, gapless |
| `protocol_md` | text | verbatim |
| `protocol_meta` | jsonb | parsed frontmatter |
| `sha256` | bytea | `GENERATED ALWAYS AS (sha256(convert_to(protocol_md, 'UTF8'))) STORED` |
| `node_id` | uuid | lineage node, kind `protocol`, label `"<slug> v<version>"` |
| `created_by`, `created_at` | | |

- `runs.protocol_version_id uuid NOT NULL` (after backfill) with FK `(tenant_id, experiment_id, protocol_version_id)` → `protocol_versions (tenant_id, experiment_id, id)`.
- `experiments.protocol_md` and `protocol_meta` are no longer written after this migration (see open question 1).
- Grants: `lde_app` gets `SELECT, INSERT` on `protocol_versions`.

API:

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/experiments` | unchanged request; also creates v1 |
| `POST` | `/v1/experiments/{id}/protocol-versions` | `{protocol_md}` → `201` with `{id, version, sha256, node_id}` |
| `GET` | `/v1/experiments/{id}/protocol-versions` | list: version, sha256, created_by, created_at |
| `GET` | `/v1/protocol-versions/{id}` | full text and meta |
| `POST` | `/v1/runs` | new optional `protocol_version_id` |

## 3. UI

Delivered with SPEC-005: the experiment view shows the current version and a version list, the run view shows the pinned version and its hash, and two versions can be diffed as text in the browser.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Hash computed by a generated column | The DB guarantees the hash matches the text; no trigger needed | Hash in Go only |
| 2026-10-18 | Protocol → run link uses the existing `used` relation | Keeps closure queries unchanged ("which runs used v3?") | PROV qualified association with `hadPlan` (more model, no current need) |
