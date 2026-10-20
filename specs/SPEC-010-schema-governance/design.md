# SPEC-010 design: Schema governance namespaces

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: a `schema_namespaces` table, keyed by the namespace text already used in SPEC-009. The linter is pure Go, comparing two schema versions. Publishing reuses change sets and signatures. The impact analysis is one SQL query over references.
- Not doing: separate tier systems, a workflow designer, or semantic diffing beyond the rules in R04.

## 2. Data and interfaces

- `schema_namespaces(tenant_id, namespace, level, description, required_for_kind, created_by, created_at)`. Level changes are revisioned (SPEC-007) and may only increase.
- `schema_versions.promoted_from uuid`. The change set op `publish_schema_version{schema, major, minor, body_text, impact_sha256?}`.
- `ontology_prefixes(tenant_id, prefix, iri_base)`.
- Signing policy for `controlled` change requests is fixed in v1: `[approval/schema_steward(ns), approval/qa]`.
- Extends SPEC-008: `signatures.entity_type` also allows `change_set`. A change request's manifest is the canonical JSON of its items, `items_sha256` and the impact analysis SHA-256. Rules R04–R07 and R11 of SPEC-008 apply unchanged. A change set stays editable until its first signature; after that, editing invalidates its signatures.
- API: `GET/POST /v1/schema-namespaces`, `POST /v1/schemas/lint` (dry run), `POST /v1/schemas/{ns}/{name}/promote`, `GET /v1/change-sets/{id}/impact`, `GET/POST /v1/ontology-prefixes`.

## 3. UI

- Change-request review: side-by-side schema diff, lint results (pass/fail per rule), rendered form preview of before and after, impact list, signatures panel, and commit/reject.
- The namespace page shows level, Stewards and pending change requests. The sandbox namespace `~<handle>` is created for each scientist on first use.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | The three tiers are levels on one registry, with a Steward per namespace (ROADMAP D4) | R&D and GMP need different strictness; one mechanism keeps one UI and one audit story | Three separate systems |
| 2026-10-20 | Change requests reuse SPEC-007 change sets and SPEC-008 signatures | One review flow for data and schemas | A schema-specific approval workflow |
