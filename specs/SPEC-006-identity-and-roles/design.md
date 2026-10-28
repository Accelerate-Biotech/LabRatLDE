# SPEC-006 design: Identity and roles

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: `go-oidc` verifies JWTs against the issuer's JWKS. New tables `principals`, `role_grants` and `agent_delegations`. `WithTenant` also sets `app.principal_id`, and `lde_has_role(role, scope)` is a SQL function that triggers and later specs call. The `identify` middleware is replaced in place.
- Not doing: a policy engine, a home-grown login, sessions stored server-side, or per-endpoint permission configuration.

## 2. Data and interfaces

### Tables

- `principals(id, tenant_id, kind ∈ {human, service, agent}, handle, display_name, email, issuer, subject, enabled, created_at)`, with `UNIQUE (tenant_id, handle)` and `UNIQUE (tenant_id, issuer, subject)`.
  - `handle` matches `^[a-z0-9][a-z0-9._@+-]{0,127}$`: lowercase, no spaces, so `<human> via <agent>` is unambiguous. `display_name` and `email` default to `''`; `issuer` and `subject` are non-empty.
  - Only `enabled` may change. `UPDATE` of any other column, `DELETE` and `TRUNCATE` fail with `LDE03`, even for the owner.
- `role_grants(id, tenant_id, principal_id, role, scope, reason, granted_by, granted_at, revoked_by, revoked_at, revoke_reason)`. Only `revoked_*` may change, once, from NULL.
  - `role ∈ {scientist, reviewer, qa, admin, schema_steward}`. `scope` is non-NULL exactly when `role = 'schema_steward'`.
  - `reason` and `revoke_reason` are non-blank. The three `revoked_*` columns are all NULL or all set.
  - One active grant per `(tenant_id, principal_id, role, scope)` (partial unique index), so a duplicate grant is `409`.
- `agent_delegations(id, tenant_id, agent_id, human_id, reason, created_by, created_at, ended_by, ended_at)`. `agent_id` must be an `agent` principal and `human_id` a `human` one. Only `ended_*` may change, once, from NULL. One active delegation per `(agent_id, human_id)`.

### Database rules

- `lde_current_principal()` and `lde_on_behalf_of()` read `app.principal_id` and `app.on_behalf_of`; NULL when unset.
- `lde_has_role(role text, scope text DEFAULT NULL) RETURNS boolean`. The current principal must be enabled and hold an active grant of `role` with exactly that `scope`. For an `agent` it also requires an active delegation from `lde_on_behalf_of()` to it, an enabled `human` on the other end, and that human holding the same grant (R08). No principal holds nothing.
- `lde_require_role(VARIADIC roles text[])` raises `LDE05` unless the principal holds one of `roles`. **It only binds `lde_app`**: the owner (migrations, `bootstrap-admin`, dev sign-in) passes, as with the `owner_maintenance` RLS policies.
- Admin gates: as `lde_app`, writing `principals`, `role_grants` or `agent_delegations` requires `admin`. For `lde_app` the triggers stamp `granted_by`/`granted_at`, `revoked_by`/`revoked_at`, `created_by` and `ended_by`/`ended_at` from the current principal and the database clock.
- Audit in the same transaction: a deferred constraint trigger on each of the three tables fails the commit with `LDE02` unless the transaction appended the matching event (`entity_type` `principal`, `role_grant` or `agent_delegation`; `entity_id` the row id; the event row's `xmin` is the current transaction). Insert expects `principal.created`, `role.granted` or `delegation.created`. Update expects `principal.disabled`/`principal.enabled`, `role.revoked` or `delegation.ended`. This binds the owner too.
- Lab-record gates (R06): statement-level triggers on `experiments`, `protocol_versions`, `lineage_nodes`, `lineage_edges`, `instruments` and `instrument_readings` call `lde_require_role('scientist')`. A row-level trigger on `runs` calls `lde_require_role('reviewer', 'qa')` for an update into `signed`, and `lde_require_role('scientist')` for everything else.
- New SQLSTATE `LDE05` = role denied, mapped to `403`. `LDE03` and `23514` (check violation) are newly mapped to `409` and `400`.
- Migrations: `0006_principals`, `0007_role_grants`, `0008_agent_delegations`, `0009_role_gates`. `0008` replaces `lde_has_role` to add the agent rule; until then agents hold nothing.

### Go

- `storage.Principal{ID, OnBehalfOf string}`. The zero value is nobody: it can read, and every role check fails.
- `storage.WithTenant(ctx, tenantID, as Principal, fn)` also sets `app.principal_id` and `app.on_behalf_of`. `storage.WithOwner(ctx, fn)` is a plain owner transaction, for bootstrap and dev sign-in only.
- `internal/identity`: `Principal`, `ValidHandle`, `Create`, `Grant`, `Revoke`, `SetEnabled`, `Delegate`, `EndDelegation`, `ByHandle`, `BySubject`. Each write appends its audit event in the caller's transaction. The API, `bootstrap-admin` and tests share it.
- `api.New(cfg, db, log, verifier)`, where `verifier` is an `api.TokenVerifier` (`*oidc.IDTokenVerifier` satisfies it), or nil in dev. In prod, `main` builds it with `oidc.NewProvider` and `provider.Verifier(&oidc.Config{ClientID: LDE_OIDC_AUDIENCE})`.
- `api.Principal` gains `As storage.Principal`, `Handle` and `Kind`. `Actor` stays the audit actor: the handle, or `<human handle> via <agent handle>`.

### Identification (`identify`)

1. **Prod**: `Authorization: Bearer <jwt>` is verified (signature, issuer, audience, expiry), or `401` with `WWW-Authenticate: Bearer`. The tenant is the `LDE_OIDC_TENANT_CLAIM` string claim and must be a UUID, or `401`. The principal is looked up by the token's `iss` and `sub` in that tenant. Missing or disabled is `403`. Dev headers are never read.
2. **Dev**: the tenant comes from `X-LDE-Tenant` and the handle is `X-LDE-Actor`, trimmed and lowercased (invalid is `401`). It's looked up by handle. If it's absent, it's created as the owner, with issuer `dev`, subject = handle, and `scientist`, `reviewer`, `qa` and `admin`, audited as actor `system:dev-auth`. A losing race re-reads it, and an unknown tenant is `401`. A disabled principal is `403`. Every dev response carries `X-LDE-Auth: dev`.
3. **Agents (both modes)**: an `agent` must send `X-LDE-On-Behalf-Of: <human handle>`, naming an enabled human with an active delegation to it, or `403`. Then `As.OnBehalfOf` is that human and `Actor` is `<human> via <agent>`. A non-agent sending the header gets `400`.

### Config

`LDE_OIDC_ISSUER`, `LDE_OIDC_AUDIENCE` (both required in prod), `LDE_OIDC_TENANT_CLAIM` (default `lde_tenant`), `LDE_OIDC_UI_CLIENT_ID`, `LDE_OIDC_UI_SCOPES` (default `openid profile email`), `LDE_UI_IDLE_TIMEOUT` (default `15m`).

### API

- `GET /v1/me`: `{id, kind, handle, display_name, email, tenant_id, actor, on_behalf_of, roles: [{id, role, scope, granted_at}], auth: "dev"|"oidc"}`.
- `GET /v1/principals` (paged by handle): principals, each with their active `roles`. `POST /v1/principals` `{kind, handle, display_name, email, issuer, subject, reason}`. `POST /v1/principals/{id}/disable` `{reason}`.
- `POST /v1/role-grants` `{principal_id, role, scope, reason}`. `POST /v1/role-grants/{id}/revoke` `{reason}`.
- `GET /v1/agent-delegations` (active ones), `POST /v1/agent-delegations` `{agent_id, human_id, reason}`, `POST /v1/agent-delegations/{id}/end` `{reason}`.
- `GET /v1/meta` (unauthenticated) adds `idle_timeout_seconds` and, in prod, `oidc: {issuer, client_id, scopes}`.
- Audit actions: `principal.created`, `principal.disabled`, `principal.enabled`, `role.granted`, `role.revoked`, `delegation.created`, `delegation.ended`.

### Bootstrap

`labrat bootstrap-admin --tenant <uuid> --issuer <iss> --subject <sub> --handle <h> [--display-name] [--email] [--slug] [--name]` connects as the owner (`LDE_DATABASE_URL`) and migrates if `LDE_AUTO_MIGRATE`. In one transaction, holding an advisory lock per tenant, it creates the tenant if absent (slug defaults to `t-<first 8 hex>`), refuses if the tenant already has an active `admin` grant, and creates the human principal with `admin` (granted by itself, reason `bootstrap-admin`), audited as actor `system:bootstrap-admin`. It prints the principal id.

## 3. UI

- Sign-in redirects to the IdP (code + PKCE). Tokens are held in memory only.
  - The PKCE verifier and state survive the redirect in `sessionStorage`, and are removed on return.
  - The redirect URI is the app's origin plus `/`.
  - The token is the `access_token`, sent as `Authorization: Bearer`. When it expires, the user signs in again (no refresh tokens).
- The CSP adds the issuer's origin to `connect-src`, for discovery and the token endpoint.
- Idle timeout (R11), in both modes: any `pointerdown`, `keydown` or `wheel` resets a timer of `idle_timeout_seconds`. When it fires, tokens (or the dev session) are discarded and sign-in shows "Signed out after N minutes of inactivity."
- A "Me" panel shows handle, display name and roles.
- Admin screens: principals list, grant/revoke roles (reason required), and delegations. They are workspace tabs (`me`, `principals`, `delegations`), reached from the explorer and the command palette.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Dependency `github.com/coreos/go-oidc/v3` (brings `golang.org/x/oauth2`) | Correct JWKS rotation and claim validation is easy to get wrong by hand | A hand-rolled JWT check |
| 2026-10-18 | Agent actor written as `<human> via <agent>` in the existing `actor` field | Records both identities without changing audit digest v1 | A new audit field (needs a new digest version) |
| 2026-10-18 | SPA uses PKCE with in-memory tokens | No server-side session store; works with any OIDC IdP | A backend-for-frontend with cookies |
| 2026-10-28 | Role checks bind `lde_app` only; the owner passes | The owner runs migrations, bootstrap and dev sign-in, and can already disable triggers; matches `owner_maintenance` | Checking the owner too (bootstrap would need a fake admin) |
| 2026-10-28 | "Audit in the same transaction" enforced by deferred constraint triggers that look for the event, raising `LDE02` | The database proves the event exists without re-implementing the chain in SQL; the API keeps calling `audit.Append` | Appending the event from a trigger (duplicates `audit.Append` and the canonical payload) |
| 2026-10-28 | `LDE02` reused for a missing audit event | `LDE06` is reserved for SPEC-009; a missing event is a broken audit link | A new code |
| 2026-10-28 | Four migrations, `0006`–`0009` | Each task's migration applies on its own, so a database that has run an earlier task's migration never needs resetting | One `0006` edited across tasks |
| 2026-10-28 | `X-LDE-On-Behalf-Of` carries the human's handle | Handles are unique, never reused and readable in logs | The principal id |
| 2026-10-28 | Shared `internal/identity` package | The API, `bootstrap-admin`, dev sign-in and tests write principals and grants the same audited way | Duplicated SQL in each |
| 2026-10-28 | `LDE_OIDC_UI_SCOPES` added | Entra and others only issue an access token for the API's audience when asked for its scope | A fixed scope list |
| 2026-10-28 | `bootstrap-admin` creates the tenant if absent | Prod has no other way to create a tenant yet | A separate `create-tenant` command |
| 2026-10-28 | API tests mint RS256 JWTs with `crypto/rsa` and verify with `oidc.StaticKeySet` | No direct dependency on a JOSE library for ~20 lines of test code | Adding `go-jose` directly |
| 2026-10-28 | `GET /v1/agent-delegations` added, and `GET /v1/principals` includes active roles | The admin screens need to list what they change, and the UI only calls public endpoints | Separate role-grant listing endpoint |
