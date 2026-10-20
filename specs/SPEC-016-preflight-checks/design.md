# SPEC-016 design: Pre-flight checks v1

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: each check is a pure Go function in `internal/domain/preflight.go` over a snapshot struct loaded in one query. The gate runs the same functions inside the transition transaction, and results go into the `run.transitioned` audit payload. "Today" comes from the database clock in the tenant time zone.
- Not doing: a rules engine, a results table, or a database-level gate. Overrides are legitimate, so this is a guarded operational rule, not a "never" invariant (decision log).

## 2. Data and interfaces

- API: `POST /v1/runs/{id}/preflight` → `{problems:[{check, severity, subject_id, message, pointer?}]}`. `POST /v1/runs/{id}/transitions` gains optional `overrides:[{check, subject_id, reason}]`. `POST /v1/runs` gains optional `instruments`.
- No new tables.

## 3. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Deterministic checks only (ROADMAP D11) | Validatable and explainable | An LLM-driven or open-ended rule base |
| 2026-10-20 | Gate enforced in Go, not by DB trigger | Overrides make it a guarded operational rule, not a "must never happen" invariant | A trigger evaluating JSON expiry dates and override records |
