# SPEC-006 tasks: Identity and roles

Requirements: [requirements.md](requirements.md) · Design: [design.md](design.md)

`make test-integration` needs `docker compose up -d`. This spec's migrations add role gates that break writes for older branches on the same database, so point `TEST_DB` at a database of its own while the spec is in flight (`CREATE DATABASE lde_spec006 OWNER lde`, then `make test-integration TEST_DB=postgres://lde:lde@localhost:5432/lde_spec006?sslmode=disable`). Every task ends with `gofmt -l .` empty and `go vet ./...` clean. UI tasks also end with `cd web && npm run build` clean.

Storage tests run SQL directly, as the owner (`db.Pool`) or as `lde_app` (`db.WithTenant`). The R05/R06/R08 fixture is built by one helper:

- `newPrincipal(t, db, tenant, handle, kind string, roles ...string) storage.Principal`
- As the owner, it inserts the principal (issuer `https://idp.test`, subject = handle) and each role grant (granted by itself, reason `fixture`), appending `principal.created` and `role.granted` events in the same transaction.

## Phase 1: Principals, grants and delegations in the database

- [x] 1.1 Test: principal handles are unique and immutable (SPEC-006-R04)
  - Requirements: SPEC-006-R04
  - Files: `internal/storage/integration_test.go`
  - Do: `TestPrincipalHandlesUniqueAndImmutable`. As the owner, insert `alice` (human) into a new tenant. Then a second `alice` in the same tenant fails with `23505`, `alice` in another tenant succeeds, and `UPDATE principals SET handle = 'al'`, `UPDATE … SET subject = 'x'` and `DELETE` each fail with `LDE03`. Use `db.Pool.Exec` directly: no audit trigger exists yet.
  - Verify: `make test-integration` fails only on the new test (the `principals` table is missing).
  - Integrity: yes
- [x] 1.2 Migration `0006_principals`
  - Requirements: SPEC-006-R04
  - Files: `internal/storage/migrations/0006_principals.sql`
  - Do:
    - `principals` exactly as in design §2, with `UNIQUE (tenant_id, id)`, `UNIQUE (tenant_id, handle)`, `UNIQUE (tenant_id, issuer, subject)`, and the handle `CHECK`.
    - `lde_current_principal()` and `lde_on_behalf_of()`: `NULLIF(current_setting(..., true), '')::uuid`, `STABLE`, granted to `lde_app`.
    - `principals_reject_mutation()`: `DELETE`, and `UPDATE` changing anything but `enabled`, raise `LDE03`. Triggers `principals_immutable` (`BEFORE UPDATE OR DELETE … FOR EACH ROW`) and `principals_no_truncate`.
    - RLS enabled and forced, with `tenant_isolation` and `owner_maintenance` policies as in `0005`. `GRANT SELECT, INSERT, UPDATE (enabled) ON principals TO lde_app`.
  - Verify: `make test-integration` passes, including 1.1.
  - Integrity: yes
- [x] 1.3 `storage.Principal`, `WithTenant(…, as, …)`, `WithOwner`
  - Requirements: SPEC-006-R05, SPEC-006-R06, SPEC-006-R08 (plumbing)
  - Files: `internal/storage/postgres.go`, `internal/storage/integration_test.go`, `internal/api/*.go` (call sites only)
  - Do:
    - `type Principal struct{ ID, OnBehalfOf string }`, documented as in design §2. `WithTenant(ctx, tenantID string, as Principal, fn)` sets `app.principal_id` and `app.on_behalf_of` in the same `set_config` statement as the tenant. Add `func (db *DB) WithOwner(ctx, fn func(pgx.Tx) error) error` (`pgx.BeginTxFunc` on the pool). Add `CodeRoleDenied = "LDE05"`.
    - `api.Principal` gains `As storage.Principal` (left zero until Phase 2). Every handler passes `p.As`.
    - Storage tests: `newTenant` also creates principal `it@lab` (as the owner, with its `principal.created` event) and records it in a package-level `sync.Map`. `as(tenant) storage.Principal` returns it, and every `db.WithTenant(ctx, x, func` becomes `db.WithTenant(ctx, x, as(x), func`.
  - Verify: `make test-integration` passes.
  - Integrity: yes
- [x] 1.4 Test: role grants are admin-only, audited and revoked once (SPEC-006-R05)
  - Requirements: SPEC-006-R05
  - Files: `internal/storage/integration_test.go`
  - Do:
    - Add `newPrincipal` (above).
    - `TestRoleGrantsAdminOnlyAndAudited`, with alice (scientist) and carol (admin) in one tenant. As alice, inserting a `reviewer` grant for herself fails with `LDE05`.
    - As carol, granting alice `reviewer` with reason "cover for bob" plus a `role.granted` audit event commits. `granted_by` is carol's id, whatever the statement passed.
    - The same grant for `qa` with no audit event fails at commit with `LDE02`, and with reason `' '` it fails with `23514`.
    - As carol, revoking with a `role.revoked` event succeeds and stamps `revoked_by`. A second revoke fails with `LDE03`, and an owner `DELETE` fails with `LDE03`.
  - Verify: `make test-integration` fails only on the new test.
  - Integrity: yes
- [x] 1.5 Migration `0007_role_grants`
  - Requirements: SPEC-006-R05
  - Files: `internal/storage/migrations/0007_role_grants.sql`
  - Do:
    - `role_grants` as in design §2, plus the partial unique index `role_grants_active`.
    - `lde_principal_holds(p uuid, r text, s text)`.
    - `lde_has_role(r text, s text DEFAULT NULL)`: true only for a non-agent principal that holds the role.
    - `lde_require_role(VARIADIC roles text[])`: a no-op unless `current_user = 'lde_app'`.
    - `role_grants_guard()` (`BEFORE INSERT OR UPDATE OR DELETE … FOR EACH ROW`):
      - `DELETE` is `LDE03`.
      - `UPDATE` of an already-revoked row, or of any column but `revoked_*`, is `LDE03`.
      - As `lde_app`, it requires `admin` and stamps the grantor and time (on insert) or the revoker and time (on update).
    - `role_grants_no_truncate`.
    - `principals_admin_only` (`BEFORE INSERT OR UPDATE`): `lde_require_role('admin')`.
    - `lde_require_audit()`: the deferred constraint trigger function from design §2. Its arguments are the entity type, the insert action, then the allowed update actions. Triggers `principals_audited` (update only `WHEN (OLD.enabled IS DISTINCT FROM NEW.enabled)`) and `role_grants_audited`.
    - RLS and policies as `0006`. `GRANT SELECT, INSERT, UPDATE (revoked_by, revoked_at, revoke_reason) ON role_grants TO lde_app`. Execute on the new functions.
  - Verify: `make test-integration` passes, including 1.4.
  - Integrity: yes
- [x] 1.6 Test: agents hold only roles shared with their human (SPEC-006-R08)
  - Requirements: SPEC-006-R08
  - Files: `internal/storage/integration_test.go`
  - Do: `TestAgentRolesIntersectHuman`, with alice (scientist), carol (admin) and bot (agent; scientist, reviewer). `has(as, role)` runs `SELECT lde_has_role($1)` in `WithTenant`.
    - With no delegation, bot acting for alice holds neither `scientist` nor `reviewer`.
    - As carol, insert a delegation alice → bot with a `delegation.created` event. Now bot for alice holds `scientist` and not `reviewer`. Bot for carol holds nothing, and bot acting for nobody holds nothing.
    - End the delegation as carol, with a `delegation.ended` event. Bot for alice holds nothing.
    - As alice, inserting a delegation fails with `LDE05`. Delegating to a human `agent_id` fails with `23514`.
  - Verify: `make test-integration` fails only on the new test.
  - Integrity: yes
- [x] 1.7 Migration `0008_agent_delegations`
  - Requirements: SPEC-006-R08
  - Files: `internal/storage/migrations/0008_agent_delegations.sql`
  - Do:
    - `agent_delegations` as in design §2, plus the partial unique index `agent_delegations_active`.
    - `agent_delegations_guard()`:
      - `DELETE` is `LDE03`, and `UPDATE` of an ended row or of any column but `ended_*` is `LDE03`.
      - On insert, check the kinds of `agent_id` and `human_id`, raising `check_violation`.
      - As `lde_app`, it requires `admin` and stamps `created_by`/`created_at` or `ended_by`/`ended_at`.
    - `agent_delegations_no_truncate` and `agent_delegations_audited`.
    - `CREATE OR REPLACE FUNCTION lde_has_role` with the agent rule from design §2.
    - RLS, policies, `GRANT SELECT, INSERT, UPDATE (ended_by, ended_at) … TO lde_app`.
  - Verify: `make test-integration` passes, including 1.6.
  - Integrity: yes

## Phase 2: Authentication in the API

- [x] 2.1 `internal/identity`
  - Requirements: SPEC-006-R05, SPEC-006-R10, SPEC-006-R12 (shared writes)
  - Files: `internal/identity/identity.go` (new)
  - Do:
    - `type Principal struct{ID, TenantID, Kind, Handle, DisplayName, Email, Issuer, Subject string; Enabled bool; CreatedAt time.Time}` with JSON tags. `var TenantRoles = []string{"scientist", "reviewer", "qa", "admin"}`. `ValidHandle(string) bool` uses the same regexp as the `CHECK`.
    - `Create(ctx, tx, actor, reason string, p Principal) (Principal, error)`.
    - `Grant(ctx, tx, actor, tenantID, principalID, role, scope, reason, grantedBy string) (string, error)`, with an empty scope stored as NULL.
    - `Revoke(ctx, tx, actor, tenantID, grantID, reason, revokedBy string) error`.
    - `SetEnabled(ctx, tx, actor, tenantID, id string, enabled bool, reason string) error`.
    - `Delegate(ctx, tx, actor, tenantID, agentID, humanID, reason, createdBy string) (string, error)` and `EndDelegation(ctx, tx, actor, tenantID, id, reason, endedBy string) error`.
    - `ByHandle(ctx, tx, handle)` and `BySubject(ctx, tx, issuer, subject)` return `pgx.ErrNoRows` when absent.
    - Every write appends its design §2 audit event in `tx` via `audit.Append`.
    - Revoke, SetEnabled and EndDelegation return `pgx.ErrNoRows` when no row matched.
  - Verify: `go build ./...`. `newPrincipal` in the storage tests keeps its own SQL.
  - Integrity: yes
- [x] 2.2 Config and the `go-oidc` dependency
  - Requirements: SPEC-006-R01
  - Files: `internal/config/config.go`, `go.mod`, `go.sum`, `cmd/server/main.go`, `internal/api/server.go`, `internal/api/integration_test.go` (call site)
  - Do:
    - Add the config fields from design §2. In prod, an empty `LDE_OIDC_ISSUER` or `LDE_OIDC_AUDIENCE` is an error.
    - `go get github.com/coreos/go-oidc/v3@v3.21.0`.
    - `api.TokenVerifier` interface, and `api.New(cfg, db, log, verifier)`. In prod, `main` builds the verifier from discovery. Prod with a nil verifier still answers `501`.
  - Verify: `go build ./...`. `LDE_ENV=prod go run ./cmd/server` without an issuer exits with the config error.
- [x] 2.3 Test: prod bearer authentication (SPEC-006-R01, SPEC-006-R02, SPEC-006-R03)
  - Requirements: SPEC-006-R01, SPEC-006-R02, SPEC-006-R03
  - Files: `internal/api/integration_test.go`
  - Do:
    - Helpers:
      - `mintToken(t, key, claims)`: RS256, hand-rolled.
      - `prodServer(t, db, key)`: `config.Config{Env: "prod", OIDCIssuer: "https://idp.test", OIDCAudience: "labrat", OIDCTenantClaim: "lde_tenant"}` with `oidc.NewVerifier(issuer, &oidc.StaticKeySet{…}, &oidc.Config{ClientID: "labrat"})`.
      - `send(t, srv, token, headers, method, path, body) (int, []byte)`.
      - `fixtureT1`: alice, bob, carol and bot via `identity` as the owner.
    - `TestBearerTokenRequired` (R01): no token, another key, expired, and wrong audience are each 401 on `POST /v1/lineage/nodes`, and T1's audit event count is unchanged.
    - `TestTenantFromTokenOnly` (R02): alice's token with `X-LDE-Tenant: <T2>` and `X-LDE-Actor: mallory` creates a node, and that node's `lineage.node_created` event is in T1 with actor `alice`.
    - `TestUnknownOrDisabledPrincipal` (R03): token for `zed` is 403. Then disable alice (`identity.SetEnabled` as the owner), and her token is 403.
  - Verify: `make test-integration` fails only on the new tests.
- [x] 2.4 `identify`: bearer tokens and dev principals
  - Requirements: SPEC-006-R01, SPEC-006-R02, SPEC-006-R03, SPEC-006-R10
  - Files: `internal/api/server.go`, `internal/api/identity.go` (new), `internal/storage/postgres.go`
  - Do:
    - Replace `identify` with steps 1 and 2 of design §2 "Identification". The lookup runs in `WithTenant(…, storage.Principal{}, …)`, and dev creation runs in `WithOwner` via `identity.Create` + `identity.Grant`.
    - Set `p.As.ID`, `p.Handle`, `p.Kind` and `p.Actor = handle`.
    - In `fail`, `LDE05` is 403. `storage.IsConflict` adds `LDE03`, and a new `storage.IsInvalid` (`23514`) is 400.
  - Verify: `make test-integration` passes, including 2.3. `curl -i` with dev headers shows `X-LDE-Auth: dev`, and a new actor appears in the audit trail as `principal.created`.
- [x] 2.5 Test: agents need a delegation and are audited as "human via agent" (SPEC-006-R07, SPEC-006-R09)
  - Requirements: SPEC-006-R07, SPEC-006-R09
  - Files: `internal/api/integration_test.go`
  - Do:
    - `TestAgentNeedsDelegation` (R07): bot's token is 403 on `GET /v1/experiments` with and without `X-LDE-On-Behalf-Of: alice`. After `identity.Delegate` alice → bot (as the owner), it's 200 with the header and 403 without. Alice sending the header is 400.
    - `TestAgentActorIsHumanViaAgent` (R09): with the delegation, bot (on behalf of alice) creates a node, and its `lineage.node_created` event has actor `alice via bot`.
  - Verify: `make test-integration` fails only on the new tests.
- [x] 2.6 `identify`: agent delegation
  - Requirements: SPEC-006-R07, SPEC-006-R09
  - Files: `internal/api/identity.go`
  - Do: step 3 of design §2 "Identification", in the same read transaction as the principal lookup.
  - Verify: `make test-integration` passes, including 2.5.

## Phase 3: Role gates

- [x] 3.1 Test: role gates on lab records (SPEC-006-R06, SPEC-006-R08)
  - Requirements: SPEC-006-R06, SPEC-006-R08
  - Files: `internal/storage/integration_test.go`
  - Do: `TestRoleGatesOnLabRecords`, with alice (scientist), bob (reviewer), carol (admin) and bot (agent; scientist, reviewer).
    - Alice creates an experiment, protocol version and run, and moves the run to `completed` (`UPDATE runs SET state`). Alice setting it `signed` fails with `LDE05`, and bob doing it succeeds.
    - Bob inserting a lineage node fails with `LDE05`, as does nobody (`storage.Principal{}`).
    - With carol's delegation alice → bot, bot for alice inserts a node, but setting a second completed run `signed` fails with `LDE05`.
  - Verify: `make test-integration` fails only on the new test.
  - Integrity: yes
- [x] 3.2 Migration `0009_role_gates`
  - Requirements: SPEC-006-R06
  - Files: `internal/storage/migrations/0009_role_gates.sql`, `internal/storage/integration_test.go` (fixture only), `cmd/server/iq.go`
  - Do:
    - `lab_records_require_scientist()` statement trigger `<table>_role_gate` on the six tables in design §2.
    - `runs_role_gate()`, a row trigger `runs_role_gate`.
    - Fixture: `newTenant`'s `it@lab` gets the four tenant roles.
    - Add every SPEC-006 trigger to `iqTriggers`.
  - Verify: `make test-integration` passes, including 3.1. `go run ./cmd/server iq` passes.
  - Integrity: yes

## Phase 4: Admin API and bootstrap

- [x] 4.1 `GET /v1/me` and `GET /v1/meta`
  - Requirements: SPEC-006-R10, SPEC-006-R11 (UI support)
  - Files: `internal/api/principals.go` (new), `internal/api/server.go`
  - Do: `/v1/me` as in design §2. `/v1/meta` adds `idle_timeout_seconds` and, in prod, `oidc`.
  - Verify: `curl` `/v1/me` with dev headers shows four roles and `auth: "dev"`. `/v1/meta` shows `idle_timeout_seconds: 900`.
- [x] 4.2 Principals endpoints
  - Requirements: SPEC-006-R03, SPEC-006-R04, SPEC-006-R05
  - Files: `internal/api/principals.go`, `internal/api/server.go`
  - Do:
    - `GET /v1/principals`: `readPage`, ordered by handle, each with active roles.
    - `POST /v1/principals`: `ValidHandle` and kind, else 400.
    - `POST /v1/principals/{id}/disable`: needs a reason.
    - All writes go through `identity` in `WithTenant(…, p.As, …)`, so a non-admin is 403 from the database.
  - Verify: as dev admin, create `zed`, list shows it, disable it, and a request as `zed` is 403. As a reviewer-only principal (made by an admin's grant + revoke), creating one is 403.
- [x] 4.3 Role grant endpoints
  - Requirements: SPEC-006-R05
  - Files: `internal/api/principals.go`, `internal/api/server.go`
  - Do: `POST /v1/role-grants` and `POST /v1/role-grants/{id}/revoke`, both needing a reason (400 if blank).
  - Verify: grant, a duplicate grant is 409, revoke, a second revoke is 409, and each shows in the audit trail with its reason.
- [x] 4.4 Delegation endpoints
  - Requirements: SPEC-006-R07, SPEC-006-R08
  - Files: `internal/api/principals.go`, `internal/api/server.go`
  - Do: `GET`/`POST /v1/agent-delegations` and `POST /v1/agent-delegations/{id}/end`. The list joins both handles. A human `agent_id` is 400.
  - Verify: create agent `bot` as dev admin, delegate yourself → bot, and a dev request as `bot` with `X-LDE-On-Behalf-Of` succeeds. End it, and it's 403.
- [x] 4.5 `labrat bootstrap-admin`
  - Requirements: SPEC-006-R12
  - Files: `cmd/server/bootstrap.go` (new), `cmd/server/main.go`
  - Do: as design §2 "Bootstrap", via `flag.NewFlagSet`. All four required flags are checked before connecting.
  - Verify: run it for a new tenant and it prints an id. Run it again and it fails with "already has an admin". Both events are in the chain, and `/v1/audit/verify` (as that tenant in dev) is valid.

## Phase 5: UI

- [x] 5.1 API client: bearer tokens and identity endpoints
  - Requirements: SPEC-006-R11 (support)
  - Files: `web/src/api.ts`
  - Do:
    - A module-level `accessToken` with `setAccessToken`. When set, `request` sends `Authorization: Bearer`; otherwise it sends the dev headers.
    - Add POST support.
    - Types and functions for `/v1/me`, principals, grants and delegations, and the extended `Meta`.
    - A 401 calls a registered `onUnauthorized` handler.
- [x] 5.2 OIDC sign-in with PKCE and CSP `connect-src`
  - Requirements: SPEC-006-R01 (UI side)
  - Files: `web/src/auth.ts` (new), `web/src/SignIn.tsx`, `web/src/App.tsx`, `web/web.go`, `internal/api/server.go`
  - Do:
    - `auth.ts`: `beginSignIn(oidc)` and `completeSignIn(oidc)`, as design §3.
    - `SignIn` shows an organisation sign-in button in prod and the dev form in dev.
    - `web.Handler(connectSrc ...string)` adds the origins to `connect-src`, and the API passes the issuer's origin.
  - Verify: `npm run build` is clean. `curl -I /` in prod shows the issuer origin in the CSP. In dev, sign-in works as before.
- [x] 5.3 Idle timeout
  - Requirements: SPEC-006-R11
  - Files: `web/src/auth.ts`, `web/src/App.tsx`
  - Do: as design §3.
  - Verify: with `LDE_UI_IDLE_TIMEOUT=1m`, an idle minute returns to sign-in with the message.
- [x] 5.4 Me, principals and delegations views
  - Requirements: SPEC-006-R05, SPEC-006-R07 (admin UI)
  - Files: `web/src/MeView.tsx`, `web/src/PrincipalsView.tsx`, `web/src/DelegationsView.tsx` (new), `web/src/tabs.ts`, `web/src/Workspace.tsx`, `web/src/Explorer.tsx`, `web/src/App.tsx`
  - Do:
    - The three tab kinds from design §3. The header shows the handle and opens "Me".
    - Principals: a table with roles, and create, disable, grant and revoke forms, where reason is required.
    - Delegations: list, create and end.
    - Errors are shown in an `role="alert"` region. The palette gets "My account", "Principals" and "Delegations".
  - Verify: as dev admin, do every action from the UI and see each in the audit viewer. Use the keyboard only.

## Phase 6: Docs

- [ ] 6.1 README, `.env.example` and `docs/`
  - Requirements: SPEC-006-R10, SPEC-006-R12
  - Files: `README.md`, `.env.example`, `docs/identity.md` (new)
  - Do:
    - Configure an IdP: the issuer, the audience, the tenant claim, the UI client with redirect `https://<host>/`, and scopes.
    - Run `bootstrap-admin`, create principals and delegate to an agent.
    - Note that dev headers work only in `LDE_ENV=dev`.
    - Update the README status row.

## Phase 7: Verify and close out

- [ ] 7.1 `verifier` passes the whole spec
- [ ] 7.2 `uat-planner` writes [uat.md](uat.md)
- [ ] 7.3 Status set to Implemented, folder moved to `specs/implemented/`, `ROADMAP.md` updated
- [ ] 7.4 Summary commit written as the PR description
