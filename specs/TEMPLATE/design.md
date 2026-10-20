# SPEC-NNN design: <Title>

<!--
How the requirements will be met. Written after requirements.md is agreed, from a reading of the current code.
Keep it to what a developer needs to build without guessing. Every choice here should trace to a requirement.
-->

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

<!-- The simplest approach that solves the problem, and what we are deliberately NOT doing to keep it simple.
     If the design needs a new dependency, package, table, service or abstraction, say why here or remove it. -->

- Approach: …
- Not doing: …

## 2. Data and interfaces

<!-- Only what this spec adds or changes: tables/columns (as a new migration), triggers and their SQLSTATE codes
     (register new codes in ROADMAP.md), API endpoints (method, path, request, response, errors), audit actions emitted.
     Use plain tables or short JSON examples. -->

## 3. UI (required from Phase 1)

<!-- Required for every spec from Phase 1 on (ROADMAP decision D7); optional for Phase 0 engine-only specs.
     Screens or interactions touched, as a short list or rough sketch; no pixel specs.
     The UI calls only public API endpoints. Name any UI flow critical enough for a Playwright test. -->

## 4. Testing approach

<!-- One line per Test: yes requirement: where its test lives (DB rules in internal/storage/integration_test.go,
     pure rules as Go unit tests, critical UI flows in Playwright). No other tests. -->

| Requirement | Test location |
|---|---|
| SPEC-NNN-R01 | |

## 5. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| | | | |
