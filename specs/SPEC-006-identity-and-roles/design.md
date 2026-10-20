# SPEC-006 design: Identity and roles

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- Approach: `go-oidc` verifies JWTs against the issuer's JWKS. New tables `principals`, `role_grants` and `agent_delegations`. `WithTenant` also sets `app.principal_id`, and `lde_has_role(role, scope)` is a SQL function that triggers and later specs call. The `identify` middleware is replaced in place.
- Not doing: a policy engine, a home-grown login, sessions stored server-side, or per-endpoint permission configuration.

## 2. Data and interfaces

- `principals(id, tenant_id, kind ∈ {human, service, agent}, handle, display_name, email, issuer, subject, enabled, created_at)`, with `UNIQUE (tenant_id, handle)` and `UNIQUE (tenant_id, issuer, subject)`.
- `role_grants(id, tenant_id, principal_id, role, scope, reason, granted_by, granted_at, revoked_by, revoked_at, revoke_reason)`. Only `revoked_*` may change, once, from NULL.
- `agent_delegations(id, tenant_id, agent_id, human_id, reason, created_by, created_at, ended_at)`.
- SQL: `lde_current_principal()` and `lde_has_role(role text, scope text DEFAULT NULL) RETURNS boolean`, which applies the R08 intersection. New SQLSTATE `LDE05` = role denied, mapped to `403`.
- `storage.WithTenant` gains a principal parameter and sets `app.principal_id`.
- Config: `LDE_OIDC_ISSUER`, `LDE_OIDC_AUDIENCE`, `LDE_OIDC_TENANT_CLAIM` (default `lde_tenant`), `LDE_OIDC_UI_CLIENT_ID`, `LDE_UI_IDLE_TIMEOUT`.
- API: `GET /v1/me`, `GET/POST /v1/principals`, `POST /v1/principals/{id}/disable`, `POST /v1/role-grants`, `POST /v1/role-grants/{id}/revoke`, `POST /v1/agent-delegations`, `POST /v1/agent-delegations/{id}/end`.
- Audit actions: `principal.created`, `principal.disabled`, `role.granted`, `role.revoked`, `delegation.created`, `delegation.ended`.

## 3. UI

- Sign-in redirects to the IdP (code + PKCE). Tokens are held in memory only.
- A "Me" panel shows handle, display name and roles.
- Admin screens: principals list, grant/revoke roles (reason required), and delegations.

## 4. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-18 | Dependency `github.com/coreos/go-oidc/v3` (brings `golang.org/x/oauth2`) | Correct JWKS rotation and claim validation is easy to get wrong by hand | A hand-rolled JWT check |
| 2026-10-18 | Agent actor written as `<human> via <agent>` in the existing `actor` field | Records both identities without changing audit digest v1 | A new audit field (needs a new digest version) |
| 2026-10-18 | SPA uses PKCE with in-memory tokens | No server-side session store; works with any OIDC IdP | A backend-for-frontend with cookies |
