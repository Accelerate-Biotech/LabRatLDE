# SPEC-001 design: Core integrity baseline

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: invariants enforced in PostgreSQL (triggers, RLS, grants), mirrored by small pure Go packages (`audit`, `domain`).
- Not doing: an event-sourcing framework, a separate audit service, a graph database, or a policy engine.

## 2. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-14 | `github.com/jackc/pgx/v5` | Needed for Postgres-specific types (arrays, jsonb, bytea), simple-protocol multi-statement migrations and SQLSTATE access | `database/sql` + `lib/pq` (in maintenance mode, weaker type support) |
| 2026-10-14 | `gopkg.in/yaml.v3` | Protocol frontmatter is YAML | Hand-rolled parser |
| 2026-10-15 | Removed Parquet export, JSON-LD export and MinIO/S3 settings | No architecture decision on export formats or archival storage yet; each needs its own spec | Keeping them as speculative scaffolding |
| 2026-10-14 | Audit hashing in both Go and PL/pgSQL | The database must reject forged events itself; Go verifies independently | Hashing in Go only (trusts the application) |
