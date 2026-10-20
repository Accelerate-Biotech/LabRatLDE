# SPEC-018 design: Audit chain anchoring

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach:
  - `crypto/ed25519` from the standard library; the signed message is a fixed text line.
  - A goroutine in the server ticks every `LDE_CHECKPOINT_INTERVAL` and writes one checkpoint per tenant whose head moved.
  - The RFC 3161 request and response use one small dependency.
  - The Python verifier checks heads against checkpoints with the stdlib only. Signature checking is documented as an `openssl pkeyutl -verify` command, so Python needs no Ed25519 code.
- Not doing: Merkle trees, a separate anchoring service, or blockchain anchoring.

## 2. Data and interfaces

- `audit_checkpoints(tenant_id, seq, hash, taken_at, key_id, signature bytea, tsa_token bytea NULL)`, PK `(tenant_id, seq)`, append-only.
- Config: `LDE_CHECKPOINT_KEY` (path to the private key file), `LDE_CHECKPOINT_DIR`, `LDE_CHECKPOINT_INTERVAL`, `LDE_TSA_URL`.
- API: `POST /v1/audit/checkpoints` (admin, on demand), `GET /v1/audit/checkpoints`, `GET /v1/audit/checkpoint-keys`. `GET /v1/audit/verify` also reports checkpoint agreement.
- CLI: `labrat keygen --out <dir>`.
- `tools/verify_audit.py` gains `--checkpoints <file>`.

## 3. UI

- The audit viewer shows the latest checkpoint per tenant and whether verification against checkpoints passed.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-20 | Signed checkpoints instead of a new digest version (ROADMAP D1) | Detects consistent rewrites without touching frozen v1 | A Merkle tree digest v2 |
| 2026-10-20 | Dependency `github.com/digitorus/timestamp` for RFC 3161 | ASN.1 request/response parsing is error-prone by hand | Hand-rolled ASN.1 |
