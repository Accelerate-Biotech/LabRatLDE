# SPEC-016: Pre-flight checks v1

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-20 / 2026-10-20 |
| **Regulatory refs** | 21 CFR 11.10(f) (operational system checks), 11.10(e) |
| **Depends on** | `SPEC-003`, `SPEC-009`, `SPEC-015` |

## 1. Problem

Runs are started with expired reagents, out-of-calibration instruments or parameters outside the protocol's limits. The mistake is usually found during review, after the physical work and materials are spent. The design's "Bio-LSP" idea is that LabRat should check a planned run like a compiler checks code, before the bench work starts. v1 keeps to deterministic checks that the data from SPEC-009 and SPEC-015 can answer.

## 2. Scope

**In scope**
- A preflight endpoint returning problems (`error` or `warning`) for a planned run.
- **Checks in v1:**
  - run params valid against the protocol version's `params_schema`;
  - each input reagent lot unexpired and `released`;
  - each associated instrument `in_service` and within calibration;
  - the protocol frontmatter's `instrument_kind` is matched by an associated instrument (warning only).
- Linking instruments to a run at creation (`wasAssociatedWith`).
- A **gate** on `planned → running`: errors block unless each one is overridden with a reason.
- Results and overrides recorded in the audit trail.
- UI: the Problems panel and an override dialog.

**Out of scope**
- Chemistry or biology rules (compatibility, concentrations, stoichiometry).
- Checks on runs already running.
- Custom user-defined rules (open question 2).
- Agents running preflight (SPEC-014 adds a read-only tool once this lands).

## 3. Simplicity budget

- Approach: each check is a pure Go function in `internal/domain/preflight.go` over a snapshot struct loaded in one query. The gate runs the same functions inside the transition transaction, and results go into the `run.transitioned` audit payload. "Today" comes from the database clock in the tenant time zone.
- Not doing: a rules engine, a results table, or a database-level gate. Overrides are legitimate, so this is a guarded operational rule, not a "never" invariant (decision log).

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-016-R01 | When preflight runs, the preflight checker shall report an `error` for each input reagent lot whose `expiry_date` is before today in the tenant time zone. | yes | material control |
| SPEC-016-R02 | When preflight runs, the preflight checker shall report an `error` for each input reagent lot whose status is not `released`. | yes | material control |
| SPEC-016-R03 | When preflight runs, the preflight checker shall report an `error` for each associated instrument whose status is not `in_service` or whose `calibration_due` is before today. | yes | equipment control |
| SPEC-016-R04 | When preflight runs, the preflight checker shall report an `error` for each schema violation of the run's params against the protocol version's `params_schema`. | yes | 11.10(h) |
| SPEC-016-R05 | When preflight runs and no associated instrument has the protocol's `instrument_kind`, the preflight checker shall report a `warning`. | no | user need |
| SPEC-016-R06 | If a run is transitioned from `planned` to `running` while preflight reports an error not covered by an override with a non-blank reason, then the run lifecycle shall reject the transition with `409` and the problem list. | yes | 11.10(f) |
| SPEC-016-R07 | When a run is transitioned from `planned` to `running`, the audit event shall include every preflight problem, each override (check, subject, reason) and the actor. | yes | 11.10(e) |
| SPEC-016-R08 | The preflight checker shall return the same problems for the same run data and the same date. | no | determinism |
| SPEC-016-R09 | When a run is created with `instruments: [ids]`, the run API shall link each instrument node to the run node with `wasAssociatedWith`. | no | lineage |
| SPEC-016-R10 | While a planned run is open in the UI, the Problems panel shall show its preflight results, the Start button shall require overrides for errors, and the override dialog shall require a reason per error. | no | user need |

## 5. Acceptance examples

Fixture: tenant time zone `Europe/London`, today 2026-10-20. Planned run `R`, protocol `params_schema` requires `cycles` with a maximum of 45, and `instrument_kind: qpcr`. Inputs: lot `L1` (released, expiry 2026-10-19), lot `L2` (quarantined, expiry 2027-01-01). Associated instrument `Q1` (qpcr, in_service, calibration_due 2026-10-01). Params `{"cycles": 50}`.

**SPEC-016-R01–R04**: preflight returns 4 errors: `lot_expired(L1)`, `lot_not_released(L2)`, `instrument_out_of_calibration(Q1)`, `params_schema(/cycles maximum)`. With `L1.expiry_date = 2026-10-20`, the expiry error disappears.

**SPEC-016-R06**: transitioning to `running` with no overrides → `409` listing the 4 errors. With overrides for all 4 but one reason blank → `409`. With 4 reasons → `running`.

**SPEC-016-R07**: that transition's audit payload contains `preflight.problems` (4 items) and `preflight.overrides` (4 items with reasons).

## 6. Data and interfaces

- API: `POST /v1/runs/{id}/preflight` → `{problems:[{check, severity, subject_id, message, pointer?}]}`. `POST /v1/runs/{id}/transitions` gains optional `overrides:[{check, subject_id, reason}]`. `POST /v1/runs` gains optional `instruments`.
- No new tables.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Who may override a preflight error: any scientist starting the run, or only reviewer/QA? Recommendation: any scientist with a reason in R&D; a later per-experiment policy for GMP. | Rob Bassett | |
| 2 | Are user-defined checks needed (e.g. "requires a positive control")? Recommendation: later, expressed in protocol frontmatter. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Deterministic checks only (ROADMAP D11) | Validatable and explainable | An LLM-driven or open-ended rule base |
| 2026-10-20 | Gate enforced in Go, not by DB trigger | Overrides make it a guarded operational rule, not a "must never happen" invariant | A trigger evaluating JSON expiry dates and override records |
