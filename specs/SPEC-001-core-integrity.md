# SPEC-001: Core integrity baseline

| | |
|---|---|
| **Status** | Implemented |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-14 / 2026-10-14 |
| **Regulatory refs** | 21 CFR 11.10(c), 11.10(e), 11.10(f), 11.50, 11.70 |
| **Depends on** | none |

## 1. Problem

The initial scaffold was built before the spec process existed. This spec records, after the fact, the invariants it already enforces, so new specs have something to build on and the tests have requirement IDs to cite. These are the rules whose failure would corrupt a scientific record or break the audit trail.

## 2. Scope

**In scope**
- Audit chain integrity, tenant isolation, lineage acyclicity and traversal, run lifecycle and signatures, chain of custody for instrument uploads.

**Out of scope**
- Authentication (dev headers only; OIDC needs its own spec).
- §11.200 signer re-authentication, external anchoring of chain heads, S3 archival, the web UI.

## 3. Simplicity budget

- Approach: invariants enforced in PostgreSQL (triggers, RLS, grants), mirrored by small pure Go packages (`audit`, `domain`).
- Not doing: an event-sourcing framework, a separate audit service, a graph database, or a policy engine.

## 4. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-001-R01 | When a tenant-owned record is created or changed, the API shall append an audit event in the same database transaction. | no | 11.10(e) |
| SPEC-001-R02 | The audit ledger shall compute each event hash exactly as `docs/audit-chain.md` v1 specifies. | yes | 11.10(e) |
| SPEC-001-R03 | The audit ledger shall serialise payloads so that equal JSON values produce identical bytes (sorted keys, numbers verbatim, no HTML escaping). | yes | 11.10(e) |
| SPEC-001-R04 | If a recorded event is altered, removed, reordered or backdated, then the audit verifier shall report the chain broken at the first affected sequence number. | yes | 11.10(c), 11.10(e) |
| SPEC-001-R05 | If an inserted audit event does not extend the tenant's chain head or its hash does not match its content, then the database shall reject the insert. | yes | 11.10(e) |
| SPEC-001-R06 | If any role attempts to update, delete or truncate audit events, then the database shall reject the statement. | yes | 11.10(c), 11.10(e) |
| SPEC-001-R07 | The audit verifier and the database function `audit_verify_chain` shall report the same head for the same chain. | yes | 11.10(e) |
| SPEC-001-R08 | While a tenant transaction is open, the database shall return only that tenant's rows. | yes | 11.10(d) |
| SPEC-001-R09 | If a new lineage edge would create a cycle, then the lineage graph shall reject it. | yes | data integrity |
| SPEC-001-R10 | The lineage graph shall return the complete upstream and downstream closure of a node. | yes | data integrity (recall impact) |
| SPEC-001-R11 | The run lifecycle shall allow only the transitions planned→running/aborted, running→paused/completed/failed/aborted, paused→running/aborted, completed/failed→signed; signed and aborted shall be terminal. | yes | 11.10(f) |
| SPEC-001-R12 | If a run is signed or aborted without a non-blank reason, then the run lifecycle shall reject the transition. | yes | 11.50 |
| SPEC-001-R13 | When an instrument stream is ingested, the ingest API shall record the SHA-256 of the exact bytes received in the audit event. | yes | chain of custody |

## 5. Acceptance examples

**SPEC-001-R02**: tenant `00000000-0000-4000-8000-000000000001`, seq 1, Unix time 1760000000 s, actor `a`, action `b`, entity_type `c`, entity_id `d`, empty reason, payload `{}`, zero prev_hash → `1c77a6a9…0817f1f5` (full value in `docs/audit-chain.md`).

**SPEC-001-R03**: `{ "b": 1, "a": {"y": 12345678901234567890, "x": "<ok>"} }` → `{"a":{"x":"<ok>","y":12345678901234567890},"b":1}`.

**SPEC-001-R04**: Given a valid 5-event chain, when event 3's actor is changed, its timestamp moved back an hour, its payload edited, event 3 deleted, or events 2 and 3 swapped, then verification fails at seq 2–4 and never passes.

**SPEC-001-R05**: Given an empty chain, when an event is inserted whose actor was changed after sealing, then the insert fails with SQLSTATE `LDE02`.

**SPEC-001-R06**: Given one event, when the app role runs `UPDATE`, `DELETE` or `TRUNCATE` on `audit_events`, each fails; when the owner runs `DELETE`, it fails with `LDE03`.

**SPEC-001-R07**: Given 5 events appended through the API path, the Go verifier and `audit_verify_chain` both report valid with the same head hash.

**SPEC-001-R08**: Given tenant A has 2 events, when tenant B counts A's events, the count is 0.

**SPEC-001-R09**: Given sample→run→raw→analysis→result, when adding result→sample or run→run, both are rejected as cycles.

**SPEC-001-R10**: In the graph above with primers→run, upstream of result is {analysis, primers, raw, run, sample}; downstream of primers is {analysis, raw, result, run}.

**SPEC-001-R11/R12**: planned→running→paused→running→completed→signed("approved") succeeds; then signed→running fails; completed→signed("  ") fails; planned→completed fails.

**SPEC-001-R13**: Given a 3-reading NDJSON body with a blank line, the summary has count 3 and a SHA-256 equal to the hash of the whole raw body.

## 9. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | none | | |

## 10. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-14 | `github.com/jackc/pgx/v5` | Needed for Postgres-specific types (arrays, jsonb, bytea), simple-protocol multi-statement migrations and SQLSTATE access | `database/sql` + `lib/pq` (in maintenance mode, weaker type support) |
| 2026-10-14 | `github.com/parquet-go/parquet-go` | Parquet is the promised open export format; writing it by hand isn't reasonable | Apache Arrow Go (much larger) |
| 2026-10-14 | `gopkg.in/yaml.v3` | Protocol frontmatter is YAML | Hand-rolled parser |
| 2026-10-14 | Audit hashing in both Go and PL/pgSQL | The database must reject forged events itself; Go verifies independently | Hashing in Go only (trusts the application) |
