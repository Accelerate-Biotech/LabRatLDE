# Audit chain specification (v1)

LabRat's audit trail is a per-tenant, append-only hash chain. This document is
normative: the Go engine (`internal/audit.Digest`), the database
(`audit_digest()` in `internal/storage/migrations/0002_audit_ledger.sql`), and
the standalone verifier (`tools/verify_audit.py`) all implement exactly this.

## Event fields

| Field         | Type         | Notes                                                        |
|---------------|--------------|--------------------------------------------------------------|
| `tenant_id`   | UUID         | lowercase, hyphenated text form                              |
| `seq`         | int64        | 1-based, gapless per tenant                                  |
| `occurred_at` | timestamp    | from the database clock, microsecond resolution              |
| `actor`       | string       | authenticated principal                                      |
| `action`      | string       | dotted verb, e.g. `run.signed`                               |
| `entity_type` | string       |                                                              |
| `entity_id`   | string       |                                                              |
| `reason`      | string       | may be empty; for signatures, the §11.50 meaning             |
| `payload`     | string       | canonical JSON (sorted keys, no whitespace), stored verbatim |
| `prev_hash`   | 32 bytes     | `hash` of event `seq-1`; 32 zero bytes for `seq = 1`         |
| `hash`        | 32 bytes     | the digest below                                             |

## Digest

```
hash = SHA-256(
    "LDE-AUDIT-v1"                       12 ASCII bytes, no prefix
    || be64(seq)
    || be64(occurred_at as Unix microseconds, two's complement)
    || lp(tenant_id) || lp(actor) || lp(action) || lp(entity_type)
    || lp(entity_id) || lp(reason)  || lp(payload)
    || prev_hash                         32 raw bytes
)

lp(s) = be64(byte length of UTF-8(s)) || UTF-8(s)
```

Length prefixes make the encoding injective without any escaping rules: no two
distinct events produce the same byte string.

Test vector (`TestDigestVector`): tenant `00000000-0000-4000-8000-000000000001`,
seq 1, `occurred_at` = Unix 1760000000 s, actor `a`, action `b`, entity_type
`c`, entity_id `d`, reason empty, payload `{}`, prev_hash = zeros →
`1c77a6a958a31b8539c0244258721ab446721171aadae8b9fca1c6830817f1f5`.

## Enforcement

On every `INSERT INTO audit_events`, a `SECURITY DEFINER` trigger:

1. locks the tenant's row in `audit_chain_heads`;
2. requires `seq = head.seq + 1` and `prev_hash = head.hash`;
3. recomputes the digest and requires it to equal `hash`;
4. requires `occurred_at` to be within the last minute of `clock_timestamp()`;
5. advances the head.

`UPDATE`, `DELETE` and `TRUNCATE` on `audit_events` raise SQLSTATE `LDE03` for
every role, including the owner, and the application role is never granted
them. Disabling the triggers requires the table owner to run DDL, which
PostgreSQL can log (`log_statement = 'ddl'`) and which is exposed by
re-verifying the chain against a previously recorded head.

## Verification

Three independent paths, which must agree:

```bash
# 1. Engine (Go) and database (PL/pgSQL), side by side
curl -s localhost:8080/v1/audit/verify -H "X-LDE-Tenant: $T" -H 'X-LDE-Actor: auditor'

# 2. Plain SQL
psql -c "SELECT * FROM audit_verify_chain('$T')"

# 3. Offline, zero shared code
curl -s localhost:8080/v1/audit/export.ndjson -H "X-LDE-Tenant: $T" -H 'X-LDE-Actor: auditor' > chain.ndjson
python3 tools/verify_audit.py chain.ndjson
```

The export carries `payload` as the exact canonical string that was hashed, so
offline verifiers never need to reproduce JSON canonicalisation.

## Threat model, honestly

A hash chain proves *integrity relative to a known head*. Someone with
superuser access to the database could disable the triggers and rewrite the
entire chain consistently. Defences, in increasing strength:

- Ship the head hash elsewhere periodically (logs, email, a ticket system).
- Anchor heads in an external transparency log or RFC 3161 timestamp authority
  *(roadmap)*.
- Replicate events to WORM object storage (S3 Object Lock) *(roadmap)*.

Any one of these turns a silent rewrite into a detectable one.
