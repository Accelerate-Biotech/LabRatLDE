# SPEC-021: Reference identity broker and IdP recipes

| | |
|---|---|
| **Status** | Draft |
| **Owner** | Rob Bassett |
| **Created / Updated** | 2026-10-29 / 2026-10-29 |
| **Regulatory refs** | 21 CFR 11.10(d), 11.100(a), 11.200(a), 11.300(b) |
| **Depends on** | `SPEC-006` |

Design: [design.md](design.md) · Tasks: [tasks.md](tasks.md)

## 1. Problem

LabRat trusts OIDC access tokens only (SPEC-006). Most target customers run Microsoft Entra ID or Okta, which speak OIDC but need careful setup: a custom tenant claim in Entra is fiddly, and Okta's org authorization server can't be used. Some sites still sign in only through SAML (older ADFS, Shibboleth, some Ping setups). Their IT teams would each have to design a SAML-to-OIDC bridge from scratch, and a subtle mistake there, such as allowing local passwords or linking accounts by email, would undermine access control (11.10(d)). LabRat should ship a tested reference broker and per-IdP setup guides, without taking SAML into its own binary.

## 2. Scope

**In scope**
- A reference broker deployment based on Keycloak. It federates one upstream IdP (SAML, or OIDC) per LabRat tenant and issues the tokens LabRat expects: the `labrat` audience, the `lde_tenant` claim, and a stable subject.
- A hardened realm template: no local passwords, no self-registration, no linking by email, signed assertions required, re-authentication passed upstream, and admin and login events recorded.
- An automated end-to-end check of the template against a test SAML IdP.
- `labrat idp-check`: diagnose why an IdP's token would or wouldn't be accepted, using the server's own verification path.
- Recipes: Entra ID and Okta directly over OIDC; any SAML IdP through the broker; Entra through the broker, for sites that can't add the tenant claim.
- Validation material for the broker: installation checks and what the org's IQ must cover.

**Out of scope**
- SAML in the LabRat binary.
- Operating, patching or clustering the broker for a customer (the org runs it; see open question 3).
- SCIM or automatic provisioning (SPEC-006 open question 1).
- Google sign-in.
- Feeding broker login events into LabRat's audit chain (open question 4).

## 3. Requirements (EARS)

| ID | Requirement | Test | Source |
|---|---|---|---|
| SPEC-021-R01 | When a user signs in through the reference broker, the broker shall issue an access token whose `aud` is the configured LabRat audience, whose `lde_tenant` is the realm's LabRat tenant, and whose `sub` is the same for that upstream identity on every sign-in. | yes | 11.10(d), 11.100(a) |
| SPEC-021-R02 | If an upstream SAML response is unsigned, has an invalid signature, is expired, or is addressed to another audience, then the broker shall refuse the sign-in and issue no token. | yes | 11.10(d) |
| SPEC-021-R03 | The reference realm shall allow no local passwords, no self-registration, and no automatic linking of an upstream identity to an existing broker account by email. | yes | 11.10(d), 11.300(b) |
| SPEC-021-R04 | When the LabRat UI asks for re-authentication (`prompt=login` or `max_age=0`), the broker shall make the upstream IdP re-authenticate the user (SAML `ForceAuthn`, OIDC `prompt=login`) before it issues a token. | yes | 11.200(a) (for SPEC-008) |
| SPEC-021-R05 | The reference realm shall record admin events and sign-in events, with retention set by the deployer (default 365 days). | no | 11.10(e) practice |
| SPEC-021-R06 | When `labrat idp-check` is given a token, it shall report whether `identify` would accept it, using the server's own verification and lookup, and name the first failing check (discovery, signature, issuer, audience, expiry, tenant claim, principal). | no | operations |
| SPEC-021-R07 | The docs shall give a recipe for each of Entra ID (OIDC), Okta (OIDC), generic SAML through the broker, and Entra through the broker. Each recipe records the date and the IdP or Keycloak version it was last verified against, on a real tenant. | no | adoption |
| SPEC-021-R08 | The broker deployment shall pin its Keycloak version, and its installation checklist shall cover that version, a digest of the realm configuration, and that R03's settings hold. | no | 11.10(a) |

## 4. Acceptance examples

Fixture: the reference broker, realm `t1` for LabRat tenant `T1`, federating a test SAML IdP with user `alice` (NameID `alice@idp.test`). LabRat runs in prod against the broker.

**SPEC-021-R01**: alice signs in twice, in separate browser sessions. Both access tokens have `aud: labrat` and `lde_tenant: T1`, and the same `sub`. LabRat resolves both to the same principal.

**SPEC-021-R02**: the test IdP sends a SAML response with the signature stripped, then one with a tampered NameID, then one whose `NotOnOrAfter` is in the past. Each ends on the broker's error page. No token reaches LabRat, and LabRat's audit chain is unchanged.

**SPEC-021-R03**: the realm's login page has no password form and no registration link. A broker user created for the email alice@idp.test isn't linked when a different upstream user with that email signs in.

**SPEC-021-R04**: alice is signed in. A request with `prompt=login` causes the test IdP to show its login form again, and the new token's `auth_time` is later than the first one's.

## 5. Open questions

| # | Question | Owner | Answer (date) |
|---|---|---|---|
| 1 | Keycloak, or another broker (Dex, Authentik, Zitadel)? Recommendation: Keycloak. Its SAML brokering is mature and widely deployed, and Dex's SAML connector is less established. | Rob Bassett | |
| 2 | One realm per LabRat tenant (tenant hardcoded), or one realm with the tenant mapped from an upstream group or attribute? Recommendation: one realm per tenant. It's the simplest, and no upstream data can move a user into another tenant. | Rob Bassett | |
| 3 | Support model: LabRat ships and tests the template, and the org operates and patches Keycloak. Is that acceptable to customers, or do we need a supported distribution? | Rob Bassett | |
| 4 | Must broker sign-in events join LabRat's audit chain for Part 11? Recommendation: no. 11.10(e) covers record changes, and sign-in records stay in the broker under the org's procedures. The validation material says so. | Rob Bassett | |
| 5 | How does a site keep subjects stable when it moves upstream IdPs (for example ADFS to Entra)? Recommendation: document re-linking broker users to the new upstream, so the broker `sub` and the LabRat principal don't change. | Rob Bassett | |
