# SPEC-008 design: Electronic signatures v2

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: a `signatures` table plus a `signature_manifests` table storing the canonical manifest text, with a generated `sha256`. Go builds the manifest using the audit package's canonical JSON. Triggers enforce policy order, roles, four-eyes, principal kind, re-auth freshness and immutability. The seal predicate becomes "run has ≥ 1 valid signature". Signing happens through one endpoint.
- Not doing: a workflow engine, configurable signature forms, or per-step deadlines and notifications (open question 3).

## 2. Data and interfaces

- `signatures(id, tenant_id, entity_type ('run'), entity_id, step, meaning, signer_id, printed_name, signed_at, manifest_id, comment, reauth_at, reauth_method ∈ {oidc, dev}, invalidated_at, invalidated_by_change_set)`.
- `signature_manifests(id, tenant_id, run_id, manifest text, sha256 GENERATED)`. Append-only.
- Meanings vocabulary (v1): `author`, `review`, `approval`, `verification`. These are stored as a check-constrained enum; adding one needs a spec.
- `experiments.signing_policy jsonb` (revisable via SPEC-007) and `runs.signing_policy jsonb` (copied at creation). Default policy: `[author/scientist, review/reviewer]`.
- New SQLSTATE `LDE07` = signature rule violation, mapped to `409`.
- API: `GET /v1/runs/{id}/manifest`, `POST /v1/runs/{id}/signatures {step, meaning, comment?}`, `GET /v1/runs/{id}/signatures`, `GET /v1/inbox/signatures` (runs where the caller can sign the next step).
- Re-auth: the UI repeats OIDC with `prompt=login&max_age=0`; the API reads `auth_time` from the token. In dev, `reauth_method = dev` is recorded and shown.

## 3. UI

- Sign dialog: a manifest summary (protocol version, params, datasets with hashes, notes), the meaning (preset by the step), an optional comment, the statement "By signing I confirm …", then IdP re-authentication, then a result.
- Sign-off inbox: runs awaiting my step, oldest first.
- The run view always shows the signatures panel. Invalidated signatures are struck through and linked to their correction.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Signature binds to a manifest digest, stored as text alongside | §11.70 linking can be re-verified later, offline | Binding to the run id only |
| 2026-10-18 | First signature seals | Data can't drift between signing steps | Sealing only at full sign-off |
| 2026-10-18 | Corrections return the run to `completed`, superseding "signed is terminal" | Corrections must be possible without obscuring history | Corrective new run only (heavy for a typo) |
