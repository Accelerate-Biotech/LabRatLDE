# SPEC-021 design: Reference identity broker and IdP recipes

Requirements: [requirements.md](requirements.md)

## 1. Simplicity budget

- **Approach:** LabRat stays OIDC-only.
  - The broker is a pinned Keycloak image with a realm template, shipped under `deploy/identity-broker/`.
  - One addition to the binary, `labrat idp-check`, reuses `identify`'s verifier and principal lookup.
  - Everything else is docs and a test harness.
  - Today's `tools/keycloak/` (a test IdP with fixed users) stays as the dev and UAT stand-in. The reference broker is a separate, hardened template.
- **Not doing:**
  - SAML in Go;
  - a LabRat-side session store;
  - a custom Keycloak extension or theme;
  - a Helm chart (compose plus docs first, Helm when a customer asks);
  - running the broker for customers.

## 2. Data and interfaces

**Artifacts** (`deploy/identity-broker/`)
- `compose.yaml`: Keycloak (pinned), with its own Postgres (separate from LabRat's), TLS terminated by the deployer's proxy.
- `realm-template.json`: one realm per LabRat tenant, with placeholders for the tenant UUID, LabRat URL, audience and upstream IdP metadata. Rendered by `render.sh` with `envsubst`.
- The realm settings that carry R01–R05:
  - local password login and registration off, and the browser flow is identity-provider redirect only (R03);
  - first-broker-login flow without "link existing account by email" (R03);
  - SAML IdP set to want assertions signed, validate the signature against the upstream certificate, and check the audience (R02);
  - `ForceAuthn` forwarded on `prompt=login` (R04);
  - client `labrat-ui` (public, PKCE S256), with redirect and post-logout URIs set to the LabRat URL;
  - an audience mapper to the LabRat audience, and a hardcoded `lde_tenant` mapper on every client (R01);
  - admin events with representation, and login events with expiry (R05).
- `checklist.md`: the IQ items for R08, including `sha256sum realm-template.json` and the rendered realm, and the Keycloak image digest.

**`labrat idp-check`** (R06)
- `labrat idp-check [--token <jwt> | --token-file <path>]` reads the same `LDE_OIDC_*` and `LDE_DATABASE_URL` config as the server.
- Steps:
  1. Discovery, then JWKS.
  2. Verify with the same `oidc.Config` `main` builds.
  3. Tenant claim present and a UUID.
  4. Principal lookup by issuer and subject: it exists and is enabled.
- It prints one line per step, `PASS`/`FAIL` with the reason, decoding (but not trusting) the token's claims to explain failures. It exits non-zero on the first failure.
- Refactor: `api.(*Server).fromToken`'s verify-and-resolve becomes a function both `identify` and `idp-check` call, so the two can't drift.

**Recipes** (`docs/identity/`): `entra-oidc.md`, `okta-oidc.md`, `saml-broker.md`, `entra-broker.md`. Each one has:
- IdP-side steps, with screenshots optional;
- the exact `LDE_OIDC_*` values;
- an `idp-check` run;
- a "Last verified: <date>, <IdP or Keycloak version>" line (R07).

Known quirks to cover:
- Entra: use v2 access tokens (`accessTokenAcceptedVersion: 2`), and the API's app ID URI as the audience. The tenant claim needs a directory extension attribute or claims-mapping policy; if that isn't possible, use the broker recipe.
- Okta: a custom authorization server (for example `/oauth2/default`), not the org server. `lde_tenant` is a custom claim on that server.

No migrations, no new SQLSTATEs, no new audit actions.

## 3. UI

No LabRat UI change. `SignIn` already sends the user to whatever issuer `/v1/meta` names. SPEC-008's re-authentication will add `prompt=login`, which R04 makes work through the broker.

## 4. Testing approach

The Test: yes rules are the broker's configuration, so they're tested end to end against a real Keycloak and a test SAML IdP, not in Go.

| Requirement | Test location |
|---|---|
| SPEC-021-R01 | `web/e2e/broker.spec.ts` (Playwright), against `deploy/identity-broker/test/compose.yaml`: broker, test SAML IdP, LabRat prod |
| SPEC-021-R02 | Same file. The test IdP fixture serves unsigned, tampered and expired responses |
| SPEC-021-R03 | Same file (login page, email-linking case), plus a check of the realm export JSON |
| SPEC-021-R04 | Same file (`prompt=login` → upstream login form, `auth_time` advances) |

`make broker-test` brings the stack up and runs the suite. The results feed `make validate` through the SPEC-005 Playwright results file.

## 5. Decision log

| Date | Decision | Why | Alternatives rejected |
|---|---|---|---|
| 2026-10-29 | Reference broker shipped and tested by LabRat, operated by the adopting org | SAML in the binary would need sessions or self-issued tokens, still wouldn't serve API clients, and adds XML-signature attack surface to validated code. Leaving it wholly to the org means every IT team re-derives a security-critical config | SAML service provider in Go; "bring your own broker" with no template |
| 2026-10-29 | Keycloak (pending open question 1) | Mature SAML brokering; already used for `tools/keycloak/` | Dex (less established SAML), Authentik, Zitadel |
| 2026-10-29 | `idp-check` shares `identify`'s code path | A diagnostic that can disagree with the server misleads IT exactly when it matters | A standalone JWT decoder |
| 2026-10-29 | Template rendered with `envsubst` | No extra dependency; works with any Keycloak version's import | Relying on Keycloak's import-time placeholder support (to confirm in implementation) |
| 2026-10-29 | Test SAML IdP: a SimpleSAMLphp container (candidate `kristophjunge/test-saml-idp`, to confirm), extended to serve bad responses | Standard SAML implementation, easy to script; fixtures for R02 | Only real IdP tenants (not reproducible in CI) |
