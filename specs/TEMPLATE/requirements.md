# SPEC-NNN: <Title>

<!--
How to use this template
- A spec is a folder: specs/SPEC-NNN-short-slug/ (next free number) holding requirements.md, design.md and tasks.md.
  Copy all three files from specs/TEMPLATE/. They are written and agreed in that order.
- When the spec is implemented and verified, the folder moves to specs/implemented/.
- Delete these comments and any sections marked (optional) that you don't need.
- Keep it short. A spec only has to be complete enough for a developer to build it without guessing.
- Requirements use EARS syntax (below). One behaviour per requirement.
- Mark `Test: yes` only for business-critical rules. Everything else is checked by review and user acceptance, not by tests.
-->

| | |
|---|---|
| **Status** | Draft · In review · Approved · Implemented · Superseded |
| **Owner** | <name> |
| **Created / Updated** | YYYY-MM-DD / YYYY-MM-DD |
| **Regulatory refs** | e.g. 21 CFR 11.10(e), 11.50 (or "none") |
| **Depends on** | e.g. `SPEC-001` (or "none") |

<!-- Status covers all three files. Approved means requirements, design and tasks are all agreed and no open question is unresolved. -->

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

<!-- Two to five sentences: who has the problem (scientist, lab manager, QA, automation engineer, auditor), what it is, why it matters now. -->

## 2. Scope

**In scope**
- …

**Out of scope** (explicitly not built in this spec)
- …

## 3. Requirements (EARS)

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
  Test: no   → checked by review and user acceptance testing.
-->

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-NNN-R01 | When <trigger>, the <system> shall <response>. | yes / no | e.g. 21 CFR 11.10(e), user interview |
| SPEC-NNN-R02 | If <unwanted condition>, then the <system> shall <response>. | yes / no | |

## 4. Acceptance examples

<!-- Only for requirements marked Test: yes. Concrete synthetic data, one example per edge that matters.
     These become the test cases, so keep them few. -->

**SPEC-NNN-R01**
- Given …
- When …
- Then …

## 5. Non-functional (optional)

<!-- Only constraints that actually apply, written in EARS where possible,
     e.g. "The ingest API shall accept 100,000 readings per request within 5 s." -->

## 6. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | | | |
