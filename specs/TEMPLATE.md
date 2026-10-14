# SPEC-NNN: <Title>

<!--
How to use this template
- Copy to specs/SPEC-NNN-short-slug.md (next free number). Delete these comments and any sections marked (optional) that you don't need.
- Keep it short. A spec only has to be complete enough for a developer to build it without guessing.
- Requirements use EARS syntax (below). One behaviour per requirement.
- Mark `Test: yes` only for business-critical rules. Everything else is checked by review, not by tests.
-->

| | |
|---|---|
| **Status** | Draft · In review · Approved · Implemented · Superseded |
| **Owner** | <name> |
| **Created / Updated** | YYYY-MM-DD / YYYY-MM-DD |
| **Regulatory refs** | e.g. 21 CFR 11.10(e), 11.50 (or "none") |
| **Depends on** | e.g. `SPEC-001` (or "none") |

## 1. Problem

<!-- Two to five sentences: who has the problem (scientist, lab manager, QA, automation engineer, auditor), what it is, why it matters now. -->

## 2. Scope

**In scope**
- …

**Out of scope** (explicitly not built in this spec)
- …

## 3. Simplicity budget

<!-- The simplest approach that solves the problem, and what we are deliberately NOT doing to keep it simple.
     If the design needs a new dependency, package, table, service or abstraction, say why here or remove it. -->

- Approach: …
- Not doing: …

## 4. Requirements (EARS)

<!--
EARS patterns. Use the simplest one that fits.

| Pattern | Template |
|---|---|
| Ubiquitous | The <system> shall <response>. |
| Event-driven | When <trigger>, the <system> shall <response>. |
| State-driven | While <state>, the <system> shall <response>. |
| Unwanted behaviour | If <unwanted condition>, then the <system> shall <response>. |
| Optional feature | Where <feature is included>, the <system> shall <response>. |
| Complex | While <state>, when <trigger>, the <system> shall <response>. |

Rules of thumb
- <system> is a named component: "the audit ledger", "the lineage graph", "the run lifecycle", "the ingest API", "the database".
- One "shall" per requirement. Make it measurable: numbers, not "fast" or "user-friendly".
- IDs are permanent: SPEC-NNN-R01, R02, … Never renumber; strike through a dropped requirement instead.
- Test: yes  → a business-critical rule; it gets an automated test that cites the ID.
  Test: no   → checked by review or manual acceptance.
-->

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-NNN-R01 | When <trigger>, the <system> shall <response>. | yes / no | e.g. 21 CFR 11.10(e), user interview |
| SPEC-NNN-R02 | If <unwanted condition>, then the <system> shall <response>. | yes / no | |

## 5. Acceptance examples

<!-- Only for requirements marked Test: yes. Concrete synthetic data, one example per edge that matters.
     These become the test cases, so keep them few. -->

**SPEC-NNN-R01**
- Given …
- When …
- Then …

## 6. Data and interfaces (optional)

<!-- Only what this spec adds or changes: tables/columns (as a new migration), API endpoints (method, path, request, response, errors), audit actions emitted.
     Use plain tables or short JSON examples. -->

## 7. UI notes (optional)

<!-- Screens or interactions touched. A short list or a rough sketch; no pixel specs. -->

## 8. Non-functional (optional)

<!-- Only constraints that actually apply, written in EARS where possible,
     e.g. "The ingest API shall accept 100,000 readings per request within 5 s." -->

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | | | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| | | | |
