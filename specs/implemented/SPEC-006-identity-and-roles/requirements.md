# SPEC-006: Identity and roles

| | |
|---|---|
| **Status** | Implemented |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-18 / 2026-10-30 |
| **Regulatory refs** | 21 CFR 11.10(d), 11.10(e), 11.10(g), 11.100(a), 11.300(a), 11.300(b) |
| **Depends on** | `SPEC-001`, `SPEC-005` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

Identity comes from two unauthenticated headers, so anyone who can reach the server can act as anyone in any tenant. That is why `LDE_ENV=prod` refuses to serve. Part 11 requires access limited to authorised individuals, unique user identities, and authority checks before a user can sign or alter records. Labs already run identity providers (Entra ID, Okta, Keycloak), so LabRat should trust them instead of storing passwords. Agents are coming (SPEC-014) and must be distinct, accountable principals.

## 2. Scope

**In scope**
- OIDC bearer-token authentication for the API, and authorization code + PKCE sign-in for the UI.
- A `principals` registry: humans, services and agents, each with a unique, never-reused handle.
- Tenant taken only from the token.
- Roles `scientist`, `reviewer`, `qa`, `admin` (tenant-wide) and `schema_steward` (scoped to a schema namespace, used by SPEC-010), granted and revoked by admins, with every change audited.
- Agent delegation: an agent acts on behalf of one human and never exceeds that human's roles.
- The database knows the current principal (`app.principal_id`) and can check roles itself.
- Role gates on existing endpoints.
- `labrat bootstrap-admin`, to create the first admin.

**Out of scope**
- Password storage, MFA and lockout (the identity provider's job).
- SCIM or automatic provisioning from the IdP (open question 1).
- Fine-grained, per-experiment permissions.
- Signing re-authentication (SPEC-008).

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-006-R01 | While `LDE_ENV=prod`, if a `/v1` request lacks a bearer token that the configured issuer signed, that is unexpired and that names the configured audience, then the API shall respond `401` without touching tenant data. | yes | 11.10(d) |
| SPEC-006-R02 | While `LDE_ENV=prod`, the API shall take the tenant only from the token's tenant claim and shall ignore `X-LDE-Tenant` and `X-LDE-Actor`. | yes | 11.10(d) |
| SPEC-006-R03 | If a token's issuer and subject do not match an enabled principal in that tenant, then the API shall respond `403`. | yes | 11.10(d), 11.100(a) |
| SPEC-006-R04 | The database shall keep principal handles unique per tenant and shall reject deleting a principal or changing its handle. | yes | 11.100(a), 11.300(a) |
| SPEC-006-R05 | The database shall record every role grant and revocation with grantor, time and reason, append an audit event in the same transaction, and reject the change unless the acting principal holds `admin`. | yes | 11.10(g) |
| SPEC-006-R06 | If a principal without `scientist` creates or changes lab records (experiments, protocol versions, lineage nodes and edges, runs, instruments and readings) other than by signing a run, or a principal without `reviewer` or `qa` transitions a run to `signed`, then the database shall reject the statement with SQLSTATE `LDE05`. | yes | 11.10(g) |
| SPEC-006-R07 | If an agent principal makes a request without an active delegation from a human principal, then the API shall respond `403`. | yes | 11.10(d) |
| SPEC-006-R08 | While an agent acts for a human, the database shall grant it only roles that both the agent and the human hold. | yes | 11.10(g) |
| SPEC-006-R09 | When an agent performs a mutation, the API shall record the audit actor as `<human handle> via <agent handle>`. | yes | 11.10(e) |
| SPEC-006-R10 | While `LDE_ENV=dev`, the API shall accept the dev headers, resolve the actor to a principal by handle (creating it with `scientist`, `reviewer`, `qa` and `admin` if absent), and mark the response with `X-LDE-Auth: dev`. | no | developer experience |
| SPEC-006-R11 | When a UI session has seen no user activity for `LDE_UI_IDLE_TIMEOUT` (default 15 min), the UI shall discard its tokens and return to sign-in. | no | 11.300(d) practice |
| SPEC-006-R12 | When `labrat bootstrap-admin --tenant <id> --issuer <iss> --subject <sub> --handle <h>` runs as the database owner and the tenant has no admin, it shall create that human principal with `admin`, and audit it. | no | operations |

## 4. Acceptance examples

Fixture: a test JWKS with key `K` and issuer `https://idp.test`. Tenant `T1` has principals `alice` (human; scientist), `bob` (human; reviewer), `carol` (human; admin) and `bot` (agent; scientist, reviewer). Tokens carry `lde_tenant`.

**SPEC-006-R01**: in prod, requests with no token, a token signed by another key, an expired token, or the wrong audience each get `401`, and the audit chain length is unchanged.

**SPEC-006-R02**: alice's token (tenant `T1`) sent with `X-LDE-Tenant: <T2>` creates a node in `T1`.

**SPEC-006-R03**: a valid token for unknown subject `zed` gets `403`. After alice's principal is disabled, her valid token gets `403`.

**SPEC-006-R04**: inserting a second `alice` in `T1` fails. `UPDATE principals SET handle='al'` and `DELETE` each fail.

**SPEC-006-R05**: alice granting herself `reviewer` fails with `LDE05`. Carol granting alice `reviewer` with reason "cover for bob" succeeds and adds a `role.granted` audit event. The same grant with no audit event in its transaction fails at commit with `LDE02`, and one with an empty reason fails. Carol revoking it succeeds once; revoking it again, or deleting it, fails with `LDE03`.

**SPEC-006-R06**: alice transitioning a completed run to `signed` fails with `LDE05`. Bob doing it succeeds. A reviewer-only principal creating a node fails with `LDE05`.

**SPEC-006-R07**: `bot` with no delegation gets `403` on any request, with or without `X-LDE-On-Behalf-Of: alice`. After carol delegates alice → bot, the request with the header succeeds and without it gets `403`.

**SPEC-006-R08**: with alice → bot delegated, `bot` acting for alice holds `scientist` (both hold it) but not `reviewer` (alice lacks it). Without a delegation, or once it has ended, `bot` holds no role. So `bot` can create a node but signing fails with `LDE05`.

**SPEC-006-R09**: that node's `lineage.node_created` event has actor `alice via bot`.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Should humans be provisioned just-in-time on first login (with no roles) or only by an admin? Recommendation: admin-provisioned in prod for GMP; JIT can come later. | Rob Bassett | Admin-provisioned in prod; dev auto-creates (R10). Recommendation adopted when the tasks were written (2026-10-28) |
| 2 | Agent delegation: bind it with the IdP's RFC 8693 `act` claim (token exchange), or via LabRat's `agent_delegations` with the agent naming the human in a header? Recommendation: the `agent_delegations` table plus an `X-LDE-On-Behalf-Of` header. It works with every IdP. | Rob Bassett | `agent_delegations` plus `X-LDE-On-Behalf-Of: <human handle>`. Recommendation adopted (2026-10-28) |
| 3 | Can one person hold `reviewer` and `qa` at once? Recommendation: yes; SPEC-008's four-eyes rule prevents the same person signing twice. | Rob Bassett | Yes. Recommendation adopted (2026-10-28) |
