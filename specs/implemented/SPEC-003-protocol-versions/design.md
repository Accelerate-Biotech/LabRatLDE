# SPEC-003 design: Immutable protocol versions

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: one `protocol_versions` table with a generated `sha256` column, an append-only trigger, a gapless-version trigger, a composite FK from `runs`, and a trigger that freezes a run's pin. One new migration, `0005_protocol_versions.sql`, which also moves existing experiment text into v1 and drops the old columns. One new handler file, `internal/api/protocols.go`.
- Not doing: a separate protocol entity independent of experiments, branching of protocols inside LabRat (authors can branch in git and push), PROV qualified associations, backfilling runs, or re-pinning.

## 2. Data and interfaces

### Migration `0005_protocol_versions.sql`

Statements in this order (the migration runs as the owner, which the `owner_maintenance` policies let past RLS):

1. **Guard.** `RAISE EXCEPTION` if `EXISTS (SELECT 1 FROM runs)` or if any experiment has `protocol_md = ''`. Message: `SPEC-003 migration needs a database with no runs and no empty protocols; reset it with: docker compose down -v` (R09).
2. **Hash helper.** `convert_to` isn't `IMMUTABLE`, so it can't appear directly in a generated column. Wrap it the way `audit_lp` does:
   `CREATE FUNCTION protocol_sha256(t text) RETURNS bytea LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$ SELECT sha256(convert_to(t, 'UTF8')) $$;` plus `GRANT EXECUTE … TO lde_app`.
3. **Table** `protocol_versions`:

| Column | Type | Notes |
|---|---|---|
| `id` | `uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY` | |
| `tenant_id` | `uuid NOT NULL` | RLS |
| `experiment_id` | `uuid NOT NULL` | FK `(tenant_id, experiment_id)` → `experiments (tenant_id, id)` |
| `version` | `int NOT NULL CHECK (version > 0)` | gapless (trigger) |
| `protocol_md` | `text NOT NULL CHECK (protocol_md <> '')` | verbatim (R12) |
| `protocol_meta` | `jsonb NOT NULL DEFAULT '{}'` | parsed frontmatter |
| `sha256` | `bytea GENERATED ALWAYS AS (protocol_sha256(protocol_md)) STORED` | R01; naming it in an INSERT is an error |
| `node_id` | `uuid NOT NULL` | FK `(tenant_id, node_id)` → `lineage_nodes`; kind `protocol`, label `"<slug> v<version>"` |
| `created_by` | `text NOT NULL` | |
| `created_at` | `timestamptz NOT NULL DEFAULT now()` | |

   Constraints: `UNIQUE (tenant_id, id)`, `UNIQUE (tenant_id, experiment_id, id)` (target of the runs FK), `UNIQUE (tenant_id, experiment_id, version)`, `UNIQUE (tenant_id, node_id)`. Index: `protocol_versions_meta_gin ON protocol_versions USING gin (protocol_meta jsonb_path_ops)` (replaces `experiments_meta_gin`).

4. **Append-only (R02).** `protocol_versions_reject_mutation()` raises `LDE03` (`'protocol version % is immutable (% rejected)'`). Triggers: `protocol_versions_append_only` `BEFORE UPDATE OR DELETE … FOR EACH ROW`, and `protocol_versions_no_truncate` `BEFORE TRUNCATE … FOR EACH STATEMENT`. A new function rather than `audit_reject_mutation()`, whose message names the audit ledger.
5. **Gapless (R03).** `protocol_versions_gapless()` `BEFORE INSERT … FOR EACH ROW`: if `NEW.version <> COALESCE((SELECT max(version) FROM protocol_versions WHERE tenant_id = NEW.tenant_id AND experiment_id = NEW.experiment_id), 0) + 1`, raise with `ERRCODE = 'check_violation'`. No lock is needed: two concurrent inserts of the same number collide on `UNIQUE (tenant_id, experiment_id, version)`, so a gap can't form.
6. **RLS and grants.** `ENABLE` and `FORCE ROW LEVEL SECURITY`, a `tenant_isolation` policy and an `owner_maintenance` policy, both exactly as in `0003_rls.sql`. `GRANT SELECT, INSERT ON protocol_versions TO lde_app`. No UPDATE or DELETE.
7. **Backfill (R09).** A `DO` block loops over `experiments ORDER BY tenant_id, created_at, id`. For each experiment it:
   - inserts a `lineage_nodes` row: `kind 'protocol'`, `label slug || ' v1'`, `created_by` = the experiment's `created_by`.
   - inserts `protocol_versions` v1 from `protocol_md` / `protocol_meta`, with `created_by` and `created_at` copied from the experiment.
   - appends an audit event for that tenant, exactly as `audit.Append` would: `seq` = head + 1 (0 if no head), `prev_hash` = head hash (32 zero bytes if none), `occurred_at = clock_timestamp()`, `actor 'system:migration-0005'`, `action 'protocol.version_created'`, `entity_type 'protocol_version'`, `entity_id` = the version id, `reason 'SPEC-003 backfill'`, `hash = audit_digest(…)`. The payload must be byte-identical to what `audit.Canonicalize` emits (keys sorted, no spaces). Built with `format()`: `{"experiment_id":"%s","node_id":"%s","sha256":"%s","version":1}`, using `encode(sha256, 'hex')`. All values are uuids or hex, so nothing needs escaping. The existing `audit_events_link` trigger re-checks the chain link and digest.
8. **Runs (R04).** `ALTER TABLE runs ADD COLUMN protocol_version_id uuid NOT NULL` (the table is empty, per the guard) and `FOREIGN KEY (tenant_id, experiment_id, protocol_version_id) REFERENCES protocol_versions (tenant_id, experiment_id, id)`.
9. **Immutable pin (R11).** `runs_reject_repin()` raises `LDE03` (`'run % protocol version is pinned'`). Trigger `runs_protocol_pinned BEFORE UPDATE OF protocol_version_id ON runs FOR EACH ROW WHEN (OLD.protocol_version_id IS DISTINCT FROM NEW.protocol_version_id)`.
10. **Drop old columns.** `DROP INDEX experiments_meta_gin; ALTER TABLE experiments DROP COLUMN protocol_md, DROP COLUMN protocol_meta;`

How this fits SPEC-002 sealing: the protocol node only ever gains `used` edges *out* to brand-new run nodes. A sealed run doesn't seal the protocol it used (only `wasGeneratedBy` outputs are sealed), so later runs can still pin a version an earlier, signed run used.

### Audit payloads

| Action | Entity | Payload keys |
|---|---|---|
| `experiment.created` | `experiment` | `slug`, `title` (`protocol_sha256` moves to the version event) |
| `protocol.version_created` | `protocol_version`, id | `experiment_id`, `node_id`, `sha256` (lowercase hex), `version` |
| `run.created` | `run` | existing keys + `protocol_version_id`, `protocol_version`, `protocol_sha256` (lowercase hex) |

### API

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/experiments` | Request unchanged. `protocol_md` empty → `400`. Creates the experiment and v1 in one transaction (two audit events). Response = the experiment read shape below |
| `GET` | `/v1/experiments/{id}` | `{id, slug, title, protocol_version_id, protocol_version, protocol_sha256, protocol_md, protocol_meta, created_by, created_at}` from the highest version (R10) |
| `POST` | `/v1/experiments/{id}/protocol-versions` | `{protocol_md}` → `201` with `{id, experiment_id, version, sha256, node_id, created_by, created_at}`. Empty → `400`; bad frontmatter → `400`; unknown experiment → `404`; same text as highest version → `409` (R13) |
| `GET` | `/v1/experiments/{id}/protocol-versions` | `[{id, version, sha256, node_id, created_by, created_at}]` ordered by `version` |
| `GET` | `/v1/protocol-versions/{id}` | full row incl. `protocol_md`, `protocol_meta` |
| `POST` | `/v1/runs` | new optional `protocol_version_id`; omitted → the experiment's highest version (R05). The lookup filters by experiment, so a version from another experiment (or an experiment with no versions) answers `404`; the composite FK still rejects it in the database (R04) |

`sha256` is always serialised as lowercase hex (`encode(sha256, 'hex')` in SQL).

Shared helper in `protocols.go`:

```go
// addProtocolVersion parses md, creates the protocol lineage node and the next
// version of experiment expID, and audits it. Used by createExperiment (v1) and
// createProtocolVersion.
func addProtocolVersion(ctx context.Context, tx pgx.Tx, p Principal, expID, slug, md string) (protocolVersion, error)
```

It reads the highest version (number and hash, for the R13 check) and inserts the next number. No lock: a concurrent add hits the unique constraint, and `fail` already maps that to `409`.

## 3. UI

Delivered with SPEC-005: the experiment view shows the current version and a version list, the run view shows the pinned version and its hash, and two versions can be diffed as text in the browser.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Hash computed by a generated column (via an `IMMUTABLE` SQL wrapper) | The DB guarantees the hash matches the text; no trigger needed | Hash in Go only |
| 2026-10-18 | Protocol → run link uses the existing `used` relation | Keeps closure queries unchanged ("which runs used v3?") | PROV qualified association with `hadPlan` (more model, no current need) |
| 2026-10-21 | No run backfill; the migration refuses to run if any run exists | Seal triggers block pinning signed/aborted runs; no regulated data can exist yet (`prod` refuses to serve) | Disabling seal triggers during the migration; leaving sealed runs unpinned with a nullable column |
| 2026-10-21 | Experiments are backfilled to v1 with audit events appended from SQL | Every mutation is audited, including migration ones | Requiring empty tables |
| 2026-10-21 | Drop `experiments.protocol_md` / `protocol_meta` in the same migration | One source of truth | Keeping them frozen (meaning differs for old vs new rows) |
| 2026-10-21 | A run's pin is frozen by trigger | Stops an unaudited re-point through `lde_app`'s UPDATE grant on `runs` | Audited re-pin endpoint while `planned` |
| 2026-10-21 | The backfill's protocol lineage node is audited by its `protocol.version_created` event (`node_id` in the payload), with no separate `lineage.node_created` | One event per backfilled version; the node exists only as part of that version | Emitting both events from SQL (more hand-built canonical payloads) |
| 2026-10-21 | First API-level integration test (`internal/api/integration_test.go`) | R06 and R07 are `Test: yes` but enforced in Go, not the DB | Marking them `Test: no` |
