# CLAUDE.md

LabRat: an open-source Laboratory Development Environment (LDE). API-first Go engine for lab work, with lineage graphs, instrument streams and a hash-chained audit trail. See `README.md` for the manifesto and architecture.

## Repository layout

| Path | What | Notes |
|---|---|---|
| `cmd/server/` | Entrypoint: config → migrate → serve | |
| `internal/domain/` | Lineage DAG, run state machine | Pure Go: no I/O, database or HTTP |
| `internal/audit/` | Hash-chain ledger + Postgres binding | Digest format is frozen (see Hard rules) |
| `internal/storage/` | pgx pool, `WithTenant` RLS boundary, embedded migrations | |
| `internal/api/`, `ingest/`, `config/` | HTTP handlers, NDJSON instrument ingest, env config | |
| `pkg/` | Small libraries other projects may import | |
| `docs/audit-chain.md` | Normative audit digest spec | |
| `specs/` | Specs that drive all new work: one folder per spec, `SPEC-NNN-slug/` | Start from `specs/TEMPLATE/`; `ROADMAP.md` reserves numbers and records design decisions |
| `specs/implemented/` | Specs that are built and verified, each with a `uat.md` | `SPEC-001` (scaffold invariants) lives here |
| `tools/verify_audit.py` | Independent chain verifier | Must share no code with the server |
| `web/` | Web UI (planned, core to the product) | Stack is decided in SPEC-005. Feature specs from Phase 1 on include their UI |

## Hard rules

- **Migrations are forward-only.** Never edit an existing file in `internal/storage/migrations/`; add the next numbered one.
- **The audit digest v1 is frozen.** Never change `audit.Digest`, `audit_digest()` or `docs/audit-chain.md` v1. A new format needs a new `DigestVersion` and a spec.
- **Every mutation is audited** with `audit.Append` inside the same `WithTenant` transaction as the change.
- **Invariants live in the database.** If something must never happen (cycles, edits to signed runs, ledger rewrites, cross-tenant reads), a constraint or trigger forbids it. Go checks are a second line, not the only one.
- Never weaken RLS, grants or append-only triggers without an Approved spec that says so.

## Simplicity first

Complexity is a cost paid by every future reader. Avoid it unless a spec requires it.

- Write the simplest thing that meets the spec. Don't build for hypothetical future requirements.
- Don't add abstraction before a second real use: no interfaces with one implementation, no factories, no generic "service/repository/manager" layers.
- Keep things flat: a few clear packages and files, not deep trees. SQL lives next to the handler that runs it.
- Prefer the standard library (`net/http`, `encoding/json`, `log/slog`, `testing`). Return plain errors and use explicit code over clever code.
- **New dependencies** need a reason in the relevant spec's Decision log. Current ones are recorded in `SPEC-001`.
- When in doubt, delete rather than add. If a change makes the code harder to explain, stop and ask.

## Specs drive the work

- Every feature or change traces to a requirement ID in `specs/` (e.g. `SPEC-003-R04`). If there's no spec, write one first (the `spec-writer` agent helps).
- A spec is a folder written and agreed in order: `requirements.md` (EARS requirements and acceptance examples), `design.md` (how, fitted to the code), `tasks.md` (phased implementation plan). Templates are in `specs/TEMPLATE/`.
- Only build specs with status **Approved**, which means all three files are agreed. Follow `tasks.md`. Don't add behaviour that isn't in the spec. If the spec is unclear, ask.
- A spec is done when the `verifier` passes and the `uat-planner` has written its `uat.md`. Its status becomes **Implemented**, its folder moves to `specs/implemented/`, and the last commit summarises the whole spec for the PR description.
- `SPEC-001` (`specs/implemented/SPEC-001-core-integrity/`) records the invariants the scaffold already enforces. New specs must not contradict it without saying so.

## Testing policy

- **Only test requirements marked `Test: yes` in a spec.** These are the business-critical rules: audit chain integrity, electronic signatures and run lifecycle, lineage correctness, tenant isolation, and chain of custody for instrument data.
- **Hundreds of arbitrary unit tests are forbidden.** No tests for getters, wiring, parsing helpers, library behaviour (pgx, yaml), HTTP plumbing or trivial code. No coverage targets.
- Put the requirement ID in a comment on each test, e.g. `// SPEC-001-R03`. One table-driven test per rule is preferred.
- A rule enforced by the database is tested against the database (`internal/storage/integration_test.go`, run with `make test-integration`).
- If a requirement seems to need a test but isn't marked `Test: yes`, say so. Don't add the test quietly.

## Data

- Never commit real patient, donor or staff identifiers. Use synthetic fixtures (`examples/`).

## Commands

```bash
docker compose up -d        # Postgres 16
go run ./cmd/server         # API on :8080, dev tenant 00000000-0000-4000-8000-000000000001
make test                   # unit tests (no database)
make test-integration       # + database guarantees; needs compose running
gofmt -l . && go vet ./...  # must be clean
```

## Agents

| Agent | Model | Use |
|---|---|---|
| `spec-writer` | opus | Interviews the user and writes a spec folder: requirements, then design, then phased tasks, each agreed before the next. Run it as the main session: `claude --agent spec-writer` |
| `work-orchestrator` | sonnet | Executes an Approved spec's `tasks.md` phase by phase through implementers, then runs the verifier and UAT planner, moves the spec to `specs/implemented/` and writes the summary commit: `claude --agent work-orchestrator`, or ask for it by name |
| `implementer` | haiku | Carries out one small, fully specified task. Dispatched by the orchestrator, on sonnet for trigger/RLS/grant work |
| `integrity-reviewer` | opus | Read-only check of a task's diff against the hard rules and SPEC-001. Dispatched by the orchestrator for storage, audit, signature and sealing tasks |
| `verifier` | opus | Read-only end-of-spec check that the branch meets every requirement, with the right tests and nothing extra. Dispatched by the orchestrator after the last task |
| `uat-planner` | sonnet | Writes `uat.md`, a user acceptance test plan for the people the spec serves. Dispatched by the orchestrator after the verifier passes |

`.claude/` and this file are local only (git-ignored); `specs/` is tracked.
