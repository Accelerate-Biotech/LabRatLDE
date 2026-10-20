# SPEC-NNN tasks: <Title>

<!--
The implementation plan. Written by the spec-writer after design.md is agreed, from a reading of the current code.
The work-orchestrator executes it phase by phase and ticks each task when it is verified and committed.

Every task must be:
- Small: one concern, usually 1–3 files, roughly under 150 lines of change. The implementer is a small model,
  so make every decision here rather than leaving it judgement calls.
- Traceable: lists the requirement IDs it delivers.
- Concrete: the exact files to create or edit; the function, type, SQL object or endpoint names and signatures;
  the expected behaviour in plain words; and the command that proves it works.
- Safe: schema changes go in a new numbered migration, never an edit to an existing one. Mutations call
  audit.Append in the same WithTenant transaction. Nothing touches the frozen audit digest v1.
- Testing-policy compliant: a test task only for a Test: yes requirement, built from its acceptance examples.
  DB-enforced rules are tested in internal/storage/integration_test.go. No other tests.
- Tests first: each Test: yes requirement gets its own test task, placed just before the code task it covers.
  The test is expected to fail until that code task lands, which must make it pass without editing the test.

Mark integrity tasks (those that write triggers, RLS policies, grants or constraints, or touch internal/storage/,
internal/audit/ or signature/sealing logic) with "Integrity: yes".

Phases run in dependency order and end with a green build: typically Database → Domain → API → UI.
Drop phases the spec doesn't need. Every requirement maps to at least one task. The final phase is fixed.
Task numbers are permanent. If the orchestrator must split a task, it adds 1.2a, 1.2b and notes why.
-->

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

## Phase 1: Database

- [ ] 1.1 Test: <rule> (SPEC-NNN-R01)
  - Requirements: SPEC-NNN-R01
  - Files: `internal/storage/integration_test.go`
  - Do: one table-driven test built from the R01 acceptance examples
  - Verify: `make test-integration` fails only on the new test
  - Integrity: yes
- [ ] 1.2 <Migration title>
  - Requirements: SPEC-NNN-R01
  - Files: `internal/storage/migrations/000N_<name>.sql`
  - Do: …
  - Verify: `make test-integration`
  - Integrity: yes

## Phase 2: API

- [ ] 2.1 …

## Phase N: Verify and close out

- [ ] N.1 `verifier` passes the whole spec
- [ ] N.2 `uat-planner` writes [uat.md](uat.md)
- [ ] N.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated
- [ ] N.4 Summary commit written as the PR description
