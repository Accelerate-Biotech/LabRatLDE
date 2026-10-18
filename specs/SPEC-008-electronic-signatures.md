# SPEC-008: Electronic signatures v2

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-18 |
| **Regulatory refs** | 21 CFR 11.10(e), 11.50, 11.70, 11.100(a), 11.200(a); EU Annex 11 §14 |
| **Depends on** | `SPEC-002`, `SPEC-003`, `SPEC-006`, `SPEC-007` |
| **Supersedes** | `SPEC-001-R12` (meaning becomes a controlled value); the "terminal" part of `SPEC-001-R11` for `signed`; `SPEC-002-R01` (sealed predicate); `SPEC-007-R10` (sealed commits) |

## 1. Problem

Today, "signing" moves a run to `signed` and stores a free-text reason. Part 11 requires more from an electronic signature:
- the signer's printed name, the date and time, and a meaning (review, approval, …) shown wherever the signed record is shown (§11.50);
- a link to the exact record content that can't be copied or excised (§11.70);
- re-authentication at signing time (§11.200).

Real labs also sign in sequence (author → reviewer → QA) and must correct signed records without obscuring what was signed. Without this, LabRat can't be used for regulated work.

## 2. Scope

**In scope**
- A `signatures` record bound to a **manifest digest** of the run as signed.
- A controlled list of meanings.
- An ordered **signing policy** per experiment, copied onto each run at creation.
- Step-up re-authentication.
- Four-eyes: no person signs two steps of the same run.
- The first signature seals the run (replacing SPEC-002's predicate).
- **Corrections:** a reviewed change set that, when committed, invalidates the run's signatures and returns it to `completed` for re-signing.
- Post-signature annotations that don't invalidate.
- Signature manifestation in the API and UI.
- The first Playwright `Test: yes` flow.

**Out of scope**
- Signing schema change requests. SPEC-010 reuses this mechanism with `entity_type = change_set`.
- Handwritten or biometric signatures.
- Signing documents outside LabRat.
- Cryptographic signatures with personal keys (PKI). LabRat's §11.70 linking comes from the manifest digest plus the hash-chained audit trail.

## 3. Simplicity budget

- Approach: a `signatures` table plus a `signature_manifests` table storing the canonical manifest text, with a generated `sha256`. Go builds the manifest using the audit package's canonical JSON. Triggers enforce policy order, roles, four-eyes, principal kind, re-auth freshness and immutability. The seal predicate becomes "run has ≥ 1 valid signature". Signing happens through one endpoint.
- Not doing: a workflow engine, configurable signature forms, or per-step deadlines and notifications (open question 3).

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-008-R01 | When a signature is applied, the signature service shall store signer principal, printed name (the signer's display name at that moment), meaning, time (database clock), policy step, and the SHA-256 of the run's canonical manifest, and append an audit event `signature.applied` whose `reason` is the meaning and whose payload carries the manifest hash. | yes | 11.50(a), 11.70 |
| SPEC-008-R02 | The run manifest shall be canonical JSON of: run id, experiment id, protocol version id and SHA-256, params, current revision numbers of the run node and its inputs, each generated dataset's id, reading count and ingest SHA-256, and each non-annotation note's id, revision and body SHA-256; equal content shall produce identical bytes. | yes | 11.70 |
| SPEC-008-R03 | If a signature's stored manifest hash differs from the SHA-256 of its stored manifest text, or the manifest names a different run, then the database shall reject the insert. | yes | 11.70 |
| SPEC-008-R04 | If a signature is applied without the signer re-authenticating within `LDE_SIGN_REAUTH_MAX_AGE` (default 120 s) before the request, then the signature service shall reject it with `401`. | yes | 11.200(a) |
| SPEC-008-R05 | If a signature does not fill the next unsigned step of the run's signing policy with that step's meaning, by a principal holding that step's role, then the database shall reject it with SQLSTATE `LDE07`. | yes | 11.10(g), 11.50 |
| SPEC-008-R06 | If a principal who already holds a valid signature on a run signs another step of it, then the database shall reject it with SQLSTATE `LDE07`. | yes | four-eyes |
| SPEC-008-R07 | If a principal of kind `agent` or `service` applies a signature, then the database shall reject it with SQLSTATE `LDE07`. | yes | 11.100(a), ROADMAP D6 |
| SPEC-008-R08 | The database shall treat a run's sealed set (SPEC-002) as sealed while the run has at least one valid signature, and the run shall move to `signed` when every policy step has a valid signature. | yes | 11.10(c) |
| SPEC-008-R09 | If a change set that touches a sealed record is committed by its author, or by a principal without `reviewer` or `qa`, then the database shall reject the commit with SQLSTATE `LDE07`. | yes | 11.10(c), four-eyes |
| SPEC-008-R10 | When a change set that touches a sealed run is committed, the signature service shall, in the same transaction, mark every valid signature on the run invalidated (with time and change set id), append a `signature.invalidated` event for each, and return the run to `completed`. | yes | 11.10(e), 11.70 |
| SPEC-008-R11 | If any role attempts to delete a signature or change any of its fields other than setting the invalidation fields once, then the database shall reject the statement with SQLSTATE `LDE03`. | yes | 11.70 |
| SPEC-008-R12 | The run view in the UI and every API representation of a run shall show each signature's printed name, date and time with time zone, meaning, and whether it is valid or invalidated, with the reason. | yes | 11.50(a), (b) |
| SPEC-008-R13 | When a run is created, the run API shall copy the experiment's signing policy onto the run, and later policy changes shall not affect existing runs. | no | predictability |
| SPEC-008-R14 | The run lifecycle shall no longer accept `to: signed` through the transitions endpoint; signing shall happen only through the signatures endpoint. | no | single path |

## 5. Acceptance examples

Fixture: experiment `E` with policy `[{step:1, meaning:"author", role:"scientist"}, {step:2, meaning:"review", role:"reviewer"}]`. Principals alice (scientist, display name "Alice Ng"), bob (reviewer, "Bob Ortiz"), dana (scientist + reviewer), bot (agent for dana). Run `R` is completed with dataset `D`.

**SPEC-008-R01 / R02**: alice signs step 1, "author", re-authenticated 30 s earlier. The signature row has printed name "Alice Ng", meaning "author", step 1, and `manifest_sha256` equal to SHA-256 of the manifest. Building the manifest twice yields identical bytes. The audit event `signature.applied` has `reason = "author"` and payload `manifest_sha256`.

**SPEC-008-R03**: inserting a signature whose `manifest_sha256` is altered by one bit fails. A manifest whose `run_id` is another run fails.

**SPEC-008-R04**: alice re-authenticated 10 minutes earlier → `401`, and no signature row is written.

**SPEC-008-R05**: bob signing step 2 before step 1 exists fails with `LDE07`. Alice (scientist only) signing step 2 fails with `LDE07`.

**SPEC-008-R06**: dana signs step 1, then dana signs step 2 → `LDE07`. Bob then signs step 2 → OK.

**SPEC-008-R07**: bot (acting for dana) signing step 1 → `LDE07`.

**SPEC-008-R08**: after alice's step-1 signature, adding an edge `X` →used→ `R` fails with `LDE04`, and `R.state` is still `completed`. After bob signs step 2, `R.state = signed`.

**SPEC-008-R09 / R10**: with `R` signed, alice proposes correcting param `anneal_temp_c` 60 → 62 (reason "transcription error"). Alice's own commit → `LDE07`. Bob commits it. Both signatures are marked invalidated by that change set, two `signature.invalidated` events are appended, `R.state = completed`, the params revision history shows 60 then 62, and the step-1 signature can be applied again.

**SPEC-008-R11**: `UPDATE signatures SET meaning = 'review'` fails with `LDE03`. Setting `invalidated_at` a second time fails with `LDE03`.

**SPEC-008-R12** (Playwright): open signed run `R`. The signatures panel shows "Alice Ng, author, 2026-10-18 14:03 UTC" and "Bob Ortiz, review, …". After the correction above, both show as "Invalidated by change set …: transcription error".

## 6. Data and interfaces

- `signatures(id, tenant_id, entity_type ('run'), entity_id, step, meaning, signer_id, printed_name, signed_at, manifest_id, comment, reauth_at, reauth_method ∈ {oidc, dev}, invalidated_at, invalidated_by_change_set)`.
- `signature_manifests(id, tenant_id, run_id, manifest text, sha256 GENERATED)`. Append-only.
- Meanings vocabulary (v1): `author`, `review`, `approval`, `verification`. These are stored as a check-constrained enum; adding one needs a spec.
- `experiments.signing_policy jsonb` (revisable via SPEC-007) and `runs.signing_policy jsonb` (copied at creation). Default policy: `[author/scientist, review/reviewer]`.
- New SQLSTATE `LDE07` = signature rule violation, mapped to `409`.
- API: `GET /v1/runs/{id}/manifest`, `POST /v1/runs/{id}/signatures {step, meaning, comment?}`, `GET /v1/runs/{id}/signatures`, `GET /v1/inbox/signatures` (runs where the caller can sign the next step).
- Re-auth: the UI repeats OIDC with `prompt=login&max_age=0`; the API reads `auth_time` from the token. In dev, `reauth_method = dev` is recorded and shown.

## 7. UI notes

- Sign dialog: a manifest summary (protocol version, params, datasets with hashes, notes), the meaning (preset by the step), an optional comment, the statement "By signing I confirm …", then IdP re-authentication, then a result.
- Sign-off inbox: runs awaiting my step, oldest first.
- The run view always shows the signatures panel. Invalidated signatures are struck through and linked to their correction.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Confirm the v1 meanings vocabulary (`author`, `review`, `approval`, `verification`). Is a QA "release" meaning needed for manufacturing? | Rob Bassett | |
| 2 | Should a correction invalidate *all* signatures or only those from steps whose content changed? Recommendation: all; simpler, and the conservative choice. | Rob Bassett | |
| 3 | Notify signers (email or webhook) when a run reaches their step? Recommendation: out of scope for v1; the inbox is enough. | Rob Bassett | |
| 4 | Does appending data to a signed run (new ingest) always need a correction, or can policy allow "append without re-sign"? Recommendation: always a correction for data, never for annotations. | Rob Bassett | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Signature binds to a manifest digest, stored as text alongside | §11.70 linking can be re-verified later, offline | Binding to the run id only |
| 2026-10-18 | First signature seals | Data can't drift between signing steps | Sealing only at full sign-off |
| 2026-10-18 | Corrections return the run to `completed`, superseding "signed is terminal" | Corrections must be possible without obscuring history | Corrective new run only (heavy for a typo) |
