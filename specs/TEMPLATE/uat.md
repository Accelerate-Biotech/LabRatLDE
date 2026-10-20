# SPEC-NNN user acceptance test plan: <Title>

<!--
Written by the uat-planner after the verifier passes. Executed by people, not by CI: the users named in the
spec's Problem section (scientist, reviewer, QA, lab manager) or someone acting for them.
Use synthetic data only. Never real patient, donor or staff identifiers.
-->

| | |
|---|---|
| **Build** | commit `<sha>` on `<branch>` |
| **Written** | YYYY-MM-DD |
| **Testers** | <roles> |

## 1. Setup

<!-- Exact steps from a clean checkout: docker compose up -d, go run ./cmd/server, dev tenant, fixtures from examples/.
     Commands are copy-pasteable. Where the feature has a UI, the cases use it; otherwise the public API. -->

## 2. Test cases

<!-- One case per user-visible behaviour. Cover every requirement, including Test: no ones: UAT is where those are
     accepted. Include the unwanted-behaviour cases ("If …, then …") as negative cases. -->

### UAT-01: <What the user is trying to do>

- **Requirements:** SPEC-NNN-R01
- **As:** <role>
- **Steps:**
  1. …
- **Expected:** …
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-NNN-R01 | UAT-01 | yes / — |

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
