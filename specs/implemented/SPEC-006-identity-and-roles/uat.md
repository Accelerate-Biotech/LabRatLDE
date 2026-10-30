# SPEC-006 user acceptance test plan: Identity and roles

| | |
|---|---|
| **Build** | branch `worktree-spec-006-identity`; application code unchanged since `62c2007` (later commits are UAT tooling and docs) |
| **Written** | 2026-10-28 |
| **Executed** | 2026-10-30 by Rob Bassett against the local Keycloak (`tools/keycloak/`): **all 16 cases passed** |
| **Testers** | Bench scientist (alice), reviewer / QA (bob), lab admin (carol), QA / validation staff checking the Part 11 controls (a developer or IdP administrator will help with the terminal and IdP steps) |

Nine of the twelve SPEC-006 requirements have automated tests (R01 to R09), but a person seeing a rule hold is still worth one case each. R10, R11 and R12 are `Test: no`: this plan is the only acceptance evidence for them.

Use synthetic data only (the handles, tenants and sample names below). Use a current Chrome, Edge or Firefox.

Two parts. Part A (UAT-01, UAT-02) runs against a development server and needs only Docker, Go and Node. Part B (UAT-03 to UAT-16) runs against a production server (`LDE_ENV=prod`) and needs a real OIDC identity provider (IdP).

## 1. Setup

### 1.0 Start from nothing (local Keycloak)

If you test against the local Keycloak in `tools/keycloak/`, which already has every user in 1.1, begin and restart here. Stop any LabRat server first.

```bash
sh tools/keycloak/reset-uat.sh        # fresh Keycloak realm, empty lde_uat006 database, UI built if missing
```

Add `--all` to also delete every other local LabRat database (it asks you to type `nuke`). Afterwards, always use a **new private browser window**: an old IdP session cookie would sign you in as the last user. With the local Keycloak, skip 1.1 and use the local lines in 1.3.

### 1.1 Prerequisites for Part B: the identity provider

An IdP administrator configures it as described in `docs/identity.md` ("Configure your identity provider"). In short:

- An API (resource) whose access tokens carry `iss` (becomes `LDE_OIDC_ISSUER`), `aud` (becomes `LDE_OIDC_AUDIENCE`), `sub`, and a string claim `lde_tenant` holding the tenant id `00000000-0000-4000-8000-0000000000a1`.
- The UI registered as a public single-page client using authorization code + PKCE, with redirect URI and post-logout redirect URI `http://localhost:8080/`. Its client id becomes `LDE_OIDC_UI_CLIENT_ID`. `LDE_OIDC_UI_SCOPES` is set so the access token is issued for the API's audience.
- CORS allowed from `http://localhost:8080` to the IdP's discovery and token endpoints.
- These test users (all with `lde_tenant` = `...0a1` unless stated), each able to sign in at the IdP:

| IdP user | Role in this plan |
|---|---|
| `alice` | scientist |
| `bob` | reviewer |
| `carol` | admin (the first admin, made in UAT-03) |
| `dave` | a person with a valid IdP login who LabRat has never been told about |
| `mallory` | an IdP user whose `lde_tenant` claim is a different tenant, `00000000-0000-4000-8000-0000000000b2` (used only in UAT-08) |
| `bot` | a client or service account for an AI agent, able to obtain an access token without a person (client credentials) |

The IdP should also advertise an `end_session_endpoint` (UAT-14). Keycloak, Okta and Entra ID do.

Note each user's `sub` claim; the plan calls them `$SUB_CAROL`, `$SUB_ALICE`, `$SUB_BOB`, `$SUB_DAVE` and `$SUB_BOT`.

### 1.2 Start the database and build the UI

From a clean checkout of the commit above, in a bash shell (Git Bash or WSL on Windows). Node 22 and Go are needed.

```bash
docker compose up -d
make ui                      # npm ci && npm run build, puts the UI in web/dist
```

### 1.3 Terminal helpers

In **every** terminal you use (server and helper), in the same checkout. With the local Keycloak, this one line sets everything below, including `LDE_DATABASE_URL`, the `SUB_*` values and a `tokens` command that exports all six `TOKEN_*` variables:

```bash
. tools/keycloak/uat.env      # then, in the helper terminal (Part B, after the server is up): tokens
```

Then define just the `api` and `claim` functions from the block below (paste from `api() {` down). **Don't run the `export` lines with the local Keycloak**: they overwrite `ISSUER` with a placeholder, and the server then fails with "lookup idp.example.org: no such host". With another IdP, run the whole block and fill in your values:

```bash
# --- Another IdP only: skip these exports with the local Keycloak ---
export LAB=http://localhost:8080
export T1=00000000-0000-4000-8000-0000000000a1      # the production tenant
export T2=00000000-0000-4000-8000-0000000000b2      # some other tenant
export ISSUER=https://idp.example.org/realms/lab/   # your IdP issuer, exactly as in the "iss" claim

# Tokens: get one access token per user from your IdP's token endpoint and export them.
# With Keycloak, for example (adapt to your IdP; tokens last only minutes, so repeat when you get 401):
#   TOKEN_ALICE=$(curl -s -d grant_type=password -d client_id=labrat-ui -d username=alice -d password='...' \
#       -d scope=openid "$ISSUER/protocol/openid-connect/token" | jq -r .access_token)
#   TOKEN_BOT=$(curl -s -d grant_type=client_credentials -d client_id=bot -d client_secret='...' \
#       "$ISSUER/protocol/openid-connect/token" | jq -r .access_token)
# Needed: TOKEN_CAROL TOKEN_ALICE TOKEN_BOB TOKEN_DAVE TOKEN_BOT TOKEN_MALLORY

api() { # api TOKEN METHOD PATH [JSON]  -> prints "body [STATUS]"
  local tok=$1 m=$2 p=$3 b=${4:-}
  curl -s -w ' [%{http_code}]\n' -X "$m" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' \
    ${b:+-d "$b"} "$LAB$p"
}
claim() { # claim TOKEN  -> prints the token's claims (needs jq)
  printf '%s' "$1" | cut -d. -f2 | tr '_-' '/+' | awk '{while(length($0)%4)$0=$0"=";print}' | base64 -d | jq .
}
```

Use `claim "$TOKEN_ALICE"` to read `sub`, and to confirm `iss`, `aud` and `lde_tenant`.

## 2. Test cases

## Part A: development server

Start the dev server in the first terminal, after sourcing `uat.env` there (1.3), which also clears any prod settings left in the shell (`dev_env` does the same later):

```bash
go run ./cmd/server          # API and UI on http://localhost:8080, LDE_ENV=dev
```

Check that the first log line says `"env":"dev"`. If it says `prod`, the sign-in page will send you to the IdP instead of showing the Tenant and Handle form: stop the server, run `dev_env` (or `unset LDE_ENV`), and start it again.

### UAT-01: Sign in on a development server and see who you are

- **Requirements:** SPEC-006-R10
- **As:** Bench scientist
- **Steps:**
  1. In a private window open `http://localhost:8080/`.
  2. Read the sign-in page. Enter Tenant `00000000-0000-4000-8000-000000000001` (prefilled) and Handle `Alice` (capital A), then click Sign in.
  3. Click `alice` in the top bar (or press Ctrl+K and choose "My account").
  4. In the terminal run
     `curl -si -H "X-LDE-Tenant: 00000000-0000-4000-8000-000000000001" -H "X-LDE-Actor: mallory" http://localhost:8080/v1/me`
  5. In the workspace, open Audit trail from the explorer and look at the most recent events.
  6. Run the curl in step 4 again.
- **Expected:**
  - Step 2: the page says it is a development server and that a new handle gets every role. After sign-in the top bar shows `alice` (lowercased).
  - Step 3: the Me tab shows Handle `alice`, Kind `human`, "Recorded in the audit trail as" `alice`, "Signed in with" "Development headers (not for regulated use)", and four roles in the Roles table: scientist, admin, reviewer and qa (any order), each Tenant-wide with a reason.
  - Step 4: HTTP `200` with the response header `X-LDE-Auth: dev`. The JSON has `"handle":"mallory"`, `"auth":"dev"`, and four roles. The principal did not exist before the request and was created by it.
  - Step 5: the audit trail shows, for each new handle, a `principal.created` event and four `role.granted` events with actor `system:dev-auth`.
  - Step 6: `200` again and no new `principal.created` event (the handle is now known).
- **Result:** Pass / Fail · Notes:

### UAT-02: An unattended session signs itself out

- **Requirements:** SPEC-006-R11
- **As:** Bench scientist and lab manager
- **Steps:**
  1. Stop the dev server (Ctrl+C) and try to start it with a too-short timeout: `LDE_UI_IDLE_TIMEOUT=30s go run ./cmd/server`.
  2. Start it with `LDE_UI_IDLE_TIMEOUT=1m go run ./cmd/server`.
  3. Run `curl -s http://localhost:8080/v1/meta`.
  4. Reload the browser, sign in as `alice` and do not touch the mouse or keyboard. Put a clock beside you.
  5. Repeat, but this time at about 40 seconds move the mouse over the page and click once. Then leave it alone again.
  6. Repeat, and this time type in the Ctrl+K palette (any text) at about 40 seconds, then leave it alone.
  7. After the last sign-out, reload the page.
- **Expected:**
  - Step 1: the server refuses to start and prints a configuration error about the idle timeout (it must be at least one minute).
  - Step 3: `{"env":"dev","idle_timeout_seconds":60}`.
  - Step 4: about one minute after your last action the workspace is replaced by the Sign in page with the message "Signed out after 1 minute of inactivity."
  - Steps 5 and 6: the one-minute clock restarts at each click or keypress, so sign-out comes about a minute after the last one, not at the first minute.
  - Step 7: you are still signed out (the session was discarded, not just hidden).
- **Result:** Pass / Fail · Notes:

Stop the dev server. Part B uses a production server.

## Part B: production server with an IdP

Start the production server in the first terminal. **With the local Keycloak**, run `prod_env && go run ./cmd/server`, then `tokens` in the helper terminal, and skip the block below. **With another IdP only**, fill in the values from your IdP (issuer exactly as in the token `iss`):

```bash
export LDE_ENV=prod
export LDE_OIDC_ISSUER=$ISSUER
export LDE_OIDC_AUDIENCE=labrat
export LDE_OIDC_UI_CLIENT_ID=labrat-ui
export LDE_OIDC_UI_SCOPES="openid profile email"      # add your API scope if the IdP needs it
# LDE_OIDC_TENANT_CLAIM defaults to lde_tenant; LDE_UI_IDLE_TIMEOUT defaults to 15m
go run ./cmd/server
```

The server must start and migrate the database. (Starting without `LDE_OIDC_ISSUER` or `LDE_OIDC_AUDIENCE` must fail with a configuration error; a developer may confirm that once.)

### UAT-03: Create the first admin for a new tenant

- **Requirements:** SPEC-006-R12
- **As:** Lab admin (with a developer, because it uses the database owner connection)
- **Steps:**
  1. In a third terminal, in the checkout, with the same `LDE_DATABASE_URL` as the server (`uat.env` sets it for the local Keycloak; otherwise the default from `.env.example` works with `docker compose`), run:
     ```bash
     go run ./cmd/server bootstrap-admin --tenant $T1 --issuer $ISSUER --subject $SUB_CAROL \
       --handle carol --display-name "Carol (synthetic admin)" --email carol@lab.example
     ```
  2. Run the same command again.
  3. Run it with `--tenant not-a-uuid`, and once more with `--handle "Carol Admin"`.
- **Expected:**
  - Step 1: prints the new principal's id and exits successfully. The tenant did not exist before; it is created.
  - Step 2: fails with a message that the tenant "already has an admin; grant further admins through the API". Nothing new is created.
  - Step 3: each fails before connecting, with a message about the tenant being a uuid and the handle being lowercase letters, digits and `._@+-`.
  - The evidence is checked in UAT-15 (events `principal.created` and `role.granted` by `system:bootstrap-admin`).
- **Result:** Pass / Fail · Notes:

### UAT-04: Sign in through the organisation's IdP; tokens are not kept

- **Requirements:** SPEC-006-R01 (UI side), SPEC-006-R03
- **As:** Lab admin (carol), with QA staff watching the browser developer tools
- **Steps:**
  1. Open the browser developer tools (F12), Network tab with "Preserve log" on, then open `http://localhost:8080/` in a private window.
  2. Read the page and click "Sign in with your organisation".
  3. In the Network tab look at the request to the IdP's authorization endpoint.
  4. Sign in as `carol` at the IdP.
  5. Back in LabRat, open the Application (Chrome/Edge) or Storage (Firefox) panel and inspect Local Storage, Session Storage and Cookies for `localhost:8080`. Look at the address bar.
  6. Click `carol` in the top bar to open "Me".
  7. Reload the page (F5).
- **Expected:**
  - Step 2: the page says "You'll sign in with your organisation's account." and has no Tenant or Handle fields.
  - Step 3: the query string has `response_type=code`, `code_challenge=…`, `code_challenge_method=S256`, a `state`, and `redirect_uri=http://localhost:8080/`.
  - Step 4: you return to LabRat and the workspace appears with `carol` in the top bar. The address bar no longer shows `?code=` or `state=`.
  - Step 5: Local Storage and Cookies hold no token (no `access_token`, `id_token` or long `eyJ…` string). Session Storage has no `labrat.oidc.pending` entry left (it is removed on return).
  - Step 6: Handle `carol`, Kind `human`, "Signed in with" the issuer URL, and one role, `admin`, with reason `bootstrap-admin`.
  - Step 7: you are returned to the Sign in page (no token survived the reload). Clicking the button signs you in again; no password is asked if your IdP session is still alive, which is fine.
- **Result:** Pass / Fail · Notes:

### UAT-05: Provision people and an agent, and grant roles with reasons

- **Requirements:** SPEC-006-R05, SPEC-006-R04
- **As:** Lab admin (carol)
- **Steps:**
  1. Signed in as carol, open Principals from the explorer.
  2. Click "New principal". Kind Human, Handle `alice`, Display name `Alice (synthetic scientist)`, Issuer = your issuer, Subject = `$SUB_ALICE`. Leave Reason empty and try to click Create.
  3. Enter the Reason `new starter` and click Create.
  4. Repeat steps 2 and 3 for `bob` (`$SUB_BOB`, reason `QA reviewer`). Then create an **Agent** (Kind Agent) with handle `bot`, subject `$SUB_BOT`, reason `lab assistant agent`. UAT-12 and UAT-13 need it. Don't create `dave`: UAT-09 needs him to stay unknown to LabRat, and principals can't be deleted.
  5. Try to create `alice` again (same handle).
  6. Try to create a human with handle `Not Valid!`. (The form lowercases letters but rejects spaces and `!`.)
  7. On the `alice` row click "Grant role", choose `scientist`, give the reason `joins the assay team`, and Grant. Do the same: `bob` gets `reviewer` (reason `signs runs`); `bot` gets `scientist` and `reviewer` (reason `agent works for the assay team`).
  8. On `alice` click "Grant role" again and check which roles are offered. Grant `scientist` a second time through the terminal. `principal_id` is alice's **LabRat** principal id, not her IdP subject (`$SUB_ALICE`; sending that gives `[422]`). With the local Keycloak, `ALICE_ID=$(pid alice)`; otherwise find the `"id"` before `"handle":"alice"` in `api $TOKEN_CAROL GET /v1/principals`. Then
     `api $TOKEN_CAROL POST /v1/role-grants "{\"principal_id\":\"$ALICE_ID\",\"role\":\"scientist\",\"reason\":\"again\"}"`.
  9. Try to grant with a blank reason through the terminal (same `$ALICE_ID`):
     `api $TOKEN_CAROL POST /v1/role-grants "{\"principal_id\":\"$ALICE_ID\",\"role\":\"qa\",\"reason\":\"  \"}"`.
- **Expected:**
  - Step 2: the Create button stays disabled (or the form refuses) until a reason is entered.
  - Step 3: "Created alice. Grant roles before they start work." and `alice` appears in the table, Enabled, with Roles "None".
  - Step 4: `bob` and `bot` appear; `bot` shows Kind `agent`.
  - Step 5: an error appears in the form (HTTP `409`, handle already taken). No second `alice` is listed.
  - Step 6: an error (HTTP `400`, invalid handle). Nothing is created.
  - Step 7: each grant shows "Granted <role> to <handle>." and the role appears in the Roles column.
  - Step 8: the form no longer offers `scientist` for alice; the duplicate through the terminal returns `[409]`.
  - Step 9: `[400]`.
  - Carol's own row still shows `admin`.
- **Result:** Pass / Fail · Notes:

### UAT-06: A scientist cannot change roles or provision people

- **Requirements:** SPEC-006-R05
- **As:** Bench scientist (alice)
- **Steps:**
  1. In a second private window open `http://localhost:8080/` and sign in as `alice` at the IdP (carol stays signed in in the first window).
  2. Click `alice` in the top bar to see your roles.
  3. Open Principals. Click "Grant role" on the `alice` row, choose `reviewer`, enter the reason `cover for bob`, and Grant.
  4. Click "New principal" and try to create `eve` (subject `eve-sub`, issuer your issuer, reason `test`).
  5. Open Agent delegations and try "New delegation" (it needs an agent and a human: choose `alice` and `bot`, reason `try`).
  6. In the terminal: `api $TOKEN_ALICE GET /v1/principals` and
     `api $TOKEN_ALICE POST /v1/role-grants "{\"principal_id\":\"$(pid alice)\",\"role\":\"reviewer\",\"reason\":\"cover for bob\"}"`
- **Expected:**
  - Step 2: only `scientist` is listed.
  - Steps 3, 4 and 5: each shows a red error in the form (HTTP `403`) and nothing changes. Reloading the Principals tab shows alice with only `scientist`, and no `eve`.
  - Step 6: the read succeeds (`[200]`, anyone signed in may read); the grant returns `[403]`.
  - In the first window, carol's Principals list (after reload) is unchanged.
- **Result:** Pass / Fail · Notes:

### UAT-07: Requests without a valid token are refused

- **Requirements:** SPEC-006-R01
- **As:** QA / validation staff (with a developer)
- **Steps:**
  1. Note the audit chain size: `api $TOKEN_CAROL GET /v1/audit/verify` and write down the Engine head `seq` (or `count`).
  2. Send a request with no token:
     `curl -si -X POST -H 'Content-Type: application/json' $LAB/v1/lineage/nodes -d '{"kind":"sample","label":"S-no-token"}'`
  3. Send a garbage token: `api "not.a.jwt" GET /v1/experiments`.
  4. Send a token for the wrong audience, signed by the IdP, if your IdP can issue one (for example from another API registration): `api $TOKEN_WRONG_AUD GET /v1/experiments`.
  5. Send an expired token: wait until `$TOKEN_ALICE` has expired (check `exp` with `claim`), then `api $TOKEN_ALICE GET /v1/experiments`.
  6. A developer alters one character in the middle of a valid token's signature, and sends it.
  7. Repeat step 1.
- **Expected:**
  - Steps 2 to 6: each returns `401`. Step 2 also shows `WWW-Authenticate: Bearer`. No node `S-no-token` exists.
  - Step 7: the audit chain size is the same as in step 1 (the refused requests wrote nothing).
  - If you cannot get a wrong-audience token, note it in Notes; that case is covered by the automated test only.
- **Result:** Pass / Fail · Notes:

### UAT-08: The tenant comes only from the token; development headers are ignored

- **Requirements:** SPEC-006-R02, SPEC-006-R01
- **As:** QA / validation staff
- **Steps:**
  1. Alice sends her valid token together with headers naming another tenant and another person:
     `curl -si -H "Authorization: Bearer $TOKEN_ALICE" -H "X-LDE-Tenant: $T2" -H "X-LDE-Actor: carol" $LAB/v1/me`
  2. Create a node the same way:
     `curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $TOKEN_ALICE" -H "X-LDE-Tenant: $T2" -H "X-LDE-Actor: carol" -H 'Content-Type: application/json' -X POST $LAB/v1/lineage/nodes -d '{"kind":"sample","label":"PBMC donor 21 (synthetic)"}'`
  3. Look at the node in the audit trail (UAT-15 shows how), or as carol: `api $TOKEN_CAROL GET '/v1/audit/events?after=0&limit=500'` and find the `lineage.node_created` event for `PBMC donor 21`.
  4. Send only the development headers, with no token:
     `curl -si -H "X-LDE-Tenant: $T1" -H "X-LDE-Actor: carol" $LAB/v1/me`
  5. Sign in to the UI as `mallory`, whose IdP token names tenant `T2`.
- **Expected:**
  - Step 1: `200`, `"handle":"alice"`, `"tenant_id"` is `$T1`, `"auth":"oidc"`, and the response has no `X-LDE-Auth` header.
  - Step 2: `201` (alice is a scientist). The node is in tenant `T1`.
  - Step 3: the event actor is `alice`, not `carol`.
  - Step 4: `401`.
  - Step 5: after the IdP login, LabRat shows the Sign in page with an error message (mallory has a valid token but no principal in tenant `T2`; `403`). Mallory sees no data of `T1`.
- **Result:** Pass / Fail · Notes:

### UAT-09: Unknown and disabled people are refused

- **Requirements:** SPEC-006-R03, SPEC-006-R04
- **As:** Lab admin (carol) and QA staff
- **Steps:**
  1. In a private window, sign in to LabRat as `dave` at the IdP (a valid login, but carol never provisioned him).
  2. In the terminal: `api $TOKEN_DAVE GET /v1/experiments`.
  3. As carol, create dave in Principals (Human, handle `dave`, issuer your issuer, subject `$SUB_DAVE`, reason `visiting scientist`). Grant him `scientist` (reason `visit`). Check that `api $TOKEN_DAVE GET /v1/me` is `200`.
  4. As carol, on the `dave` row click Disable, enter the reason `visit ended`, and confirm.
  5. Run `api $TOKEN_DAVE GET /v1/experiments` again, using the same, still valid token. Try to sign in to the UI as dave once more.
  6. Look at the `dave` row. Try to create a new principal with handle `dave` (different subject).
  7. On the `carol` row click Disable.
- **Expected:**
  - Steps 1 and 2: the UI returns to Sign in with an error, and the API answers `[403]`. Dave sees no data.
  - Step 3: dave can now sign in and `GET /v1/me` returns `[200]`.
  - Step 4: "Disabled dave. They can no longer sign in." The row shows Disabled and offers no actions.
  - Step 5: `[403]` from the API, and the UI refuses at sign-in.
  - Step 6: the handle stays taken: creating another `dave` fails with `409`. There is no way to delete or rename a principal from the UI.
  - Step 7: carol cannot disable herself: an error (HTTP `400`), and she stays Enabled.
- **Result:** Pass / Fail · Notes:

### UAT-10: A scientist cannot sign, and a reviewer cannot record work

- **Requirements:** SPEC-006-R06
- **As:** Bench scientist (alice) and reviewer (bob)
- **Steps:**
  1. As alice (terminal), create an experiment and a run, and complete it:
     ```bash
     EXP=$(api $TOKEN_ALICE POST /v1/experiments '{"slug":"il6-qpcr","title":"IL-6 qPCR (synthetic)","protocol_md":"---\nprotocol: qpcr\nversion: 1\n---\n\n# Steps\n1. Plate triplicates.\n"}' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
     RUN=$(api $TOKEN_ALICE POST /v1/runs "{\"experiment_id\":\"$EXP\",\"label\":\"Plate 7\"}" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
     api $TOKEN_ALICE POST /v1/runs/$RUN/transitions '{"to":"running"}'
     api $TOKEN_ALICE POST /v1/runs/$RUN/transitions '{"to":"completed"}'
     ```
  2. Alice tries to sign her own run:
     `api $TOKEN_ALICE POST /v1/runs/$RUN/transitions '{"to":"signed","reason":"self-approval"}'`
  3. Bob tries to record work:
     `api $TOKEN_BOB POST /v1/lineage/nodes '{"kind":"sample","label":"S-bob"}'` and
     `api $TOKEN_BOB POST /v1/experiments '{"slug":"bobs-exp","title":"x","protocol_md":"---\nprotocol: x\n---\n"}'`
  4. Bob reads: `api $TOKEN_BOB GET /v1/experiments`.
  5. Bob signs the run:
     `api $TOKEN_BOB POST /v1/runs/$RUN/transitions '{"to":"signed","reason":"Reviewed, results accepted"}'`
  6. In the UI as bob, open the experiment `il6-qpcr` and run `Plate 7`.
- **Expected:**
  - Step 1: each call succeeds (`[201]` or `[200]`).
  - Step 2: `[403]` with an `error` message. The run stays `completed`.
  - Step 3: both return `[403]`. No `S-bob` node and no `bobs-exp` experiment exist (check as alice or in the explorer).
  - Step 4: `[200]` with the experiments (any signed-in person may read).
  - Step 5: `[200]`; the run is `signed`.
  - Step 6: the run view shows State `signed`, and the State history shows the signature with bob as actor and the reason.
  - Not reachable from the terminal: a reviewer changing other run fields while signing. The API accepts only a state change, so this rule is proven by the automated test `TestSigningChangesNothingElse`.
- **Result:** Pass / Fail · Notes:

### UAT-11: Revoke a role and see it take effect

- **Requirements:** SPEC-006-R05, SPEC-006-R06
- **As:** Lab admin (carol), reviewer (bob)
- **Steps:**
  1. As alice, create and complete a second run in the same experiment. Don't repeat the `EXP=` line from UAT-10 (the slug already exists, so it returns `409` and leaves `$EXP` empty). With the local Keycloak: `RUN2=$(completed_run "Plate 8")`; otherwise run the `RUN=` and two transition lines from UAT-10 step 1 with the experiment's id. Check `echo $RUN2` prints an id: an empty one makes later URLs return `307`.
  2. As carol, in Principals, click Revoke beside `reviewer` on `bob`'s row. Try to submit with an empty reason, then give `rotated to another project`.
  3. Bob tries to sign: `api $TOKEN_BOB POST /v1/runs/$RUN2/transitions '{"to":"signed","reason":"sign after revoke"}'`
  4. Revoke the same grant again through the terminal. Revoked grants no longer appear in `/v1/principals`, so take the id from the audit trail: `GRANT_ID=$(last_revoked)` with the local Keycloak (otherwise the `entity_id` of the latest `role.revoked` event), then
     `api $TOKEN_CAROL POST /v1/role-grants/$GRANT_ID/revoke '{"reason":"again"}'`
  5. As carol, grant `reviewer` to bob again (reason `back on QA rota`). Bob retries step 3.
- **Expected:**
  - Step 2: the form refuses an empty reason. After the revoke, "Revoked reviewer from bob." and bob's roles show "None".
  - Step 3: `[403]`. The run stays `completed`.
  - Step 4: `[409]`; a revoked grant can be revoked only once.
  - Step 5: the grant succeeds, and bob's signing now returns `[200]`. (The history now holds the first grant revoked and a new grant; the old one is never reused or deleted.)
- **Result:** Pass / Fail · Notes:

### UAT-12: An agent with no delegation can do nothing

- **Requirements:** SPEC-006-R07
- **As:** Lab admin (carol) and QA staff
- **Before you start:** `bot` must exist as an agent with `scientist` and `reviewer` (UAT-05 steps 4 and 7). In Principals, its row shows Kind `agent` and both roles; with the local Keycloak, `pid bot` prints an id. Without it, every bot request is `403` because LabRat doesn't know the agent at all, and this case proves nothing.
- **Steps:**
  1. In Agent delegations, check that the list says "No active delegations."
  2. With the agent's own token, with and without naming a human:
     `api $TOKEN_BOT GET /v1/experiments`
     `curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $TOKEN_BOT" -H 'X-LDE-On-Behalf-Of: alice' $LAB/v1/experiments`
  3. A human that is not an agent sends the header: `curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $TOKEN_ALICE" -H 'X-LDE-On-Behalf-Of: alice' $LAB/v1/me`
- **Expected:**
  - Step 2: both are `[403]`: reading is not even possible for an agent without a delegation.
  - Step 3: `[400]`.
- **Result:** Pass / Fail · Notes:

### UAT-13: A delegated agent acts for one human, with only that human's roles

- **Requirements:** SPEC-006-R07, SPEC-006-R08, SPEC-006-R09
- **As:** Lab admin (carol) and bench scientist (alice)
- **Before you start:** the same `bot` check as UAT-12. If the New delegation form says it needs an enabled agent, `bot` was never created: go back to UAT-05 steps 4 and 7.
- **Steps:**
  1. As carol, open Agent delegations, click "New delegation", choose Human `alice`, Agent `bot`, reason `assay automation for alice`, and Delegate.
  2. Bot creates a node on alice's behalf:
     `curl -s -w ' [%{http_code}]\n' -X POST -H "Authorization: Bearer $TOKEN_BOT" -H 'X-LDE-On-Behalf-Of: alice' -H 'Content-Type: application/json' $LAB/v1/lineage/nodes -d '{"kind":"sample","label":"PBMC donor 22 (synthetic)"}'`
  3. Bot, same token, no header: `api $TOKEN_BOT GET /v1/experiments`.
  4. Bot names a different human: `curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $TOKEN_BOT" -H 'X-LDE-On-Behalf-Of: bob' $LAB/v1/experiments`.
  5. Bot tries to sign a completed run for alice. First, as alice, `RUN3=$(completed_run "Plate 9")` (see UAT-11 step 1; run `tokens` first if more than 15 minutes have passed), and check `echo $RUN3` prints an id. Then:
     `curl -s -w ' [%{http_code}]\n' -X POST -H "Authorization: Bearer $TOKEN_BOT" -H 'X-LDE-On-Behalf-Of: alice' -H 'Content-Type: application/json' $LAB/v1/runs/$RUN3/transitions -d '{"to":"signed","reason":"agent sign"}'`
  6. Check the audit event for the node from step 2 (see UAT-15, or `api $TOKEN_CAROL GET '/v1/audit/events?after=0&limit=500'`).
  7. Bot reads its own record: `curl -s -H "Authorization: Bearer $TOKEN_BOT" -H 'X-LDE-On-Behalf-Of: alice' $LAB/v1/me`.
  8. As carol, in Agent delegations click End beside `alice → bot`, enter the reason `automation finished`, and confirm. Repeat steps 2 and 3.
- **Expected:**
  - Step 1: "bot can now act for alice." and the delegation is listed with its reason and time.
  - Step 2: `[201]`. Alice and the bot both hold `scientist`.
  - Step 3: `[403]` (the header is required).
  - Step 4: `[403]` (no delegation from bob to bot).
  - Step 5: `[403]`. The bot holds `reviewer`, but alice does not, and the agent never holds more than the human it acts for. The run stays `completed`.
  - Step 6: the `lineage.node_created` event for `PBMC donor 22` has actor `alice via bot`.
  - Step 7: `"on_behalf_of":"alice"`, `"actor":"alice via bot"`, and `roles` containing the bot's own grants.
  - Step 8: "bot can no longer act for alice." The list shows "No active delegations." Both retries return `[403]`.
- **Result:** Pass / Fail · Notes:

### UAT-14: Sign-out ends the IdP session, and a production session times out

- **Requirements:** SPEC-006-R11 (also the IdP sign-out decision in design.md section 4)
- **As:** Bench scientist (alice) on a shared bench computer; lab manager
- **Steps:**
  1. In a private window sign in as alice. Click Sign out in the top bar.
  2. Observe where the browser goes. Then click "Sign in with your organisation" again.
  3. Stop the production server and restart it with `LDE_UI_IDLE_TIMEOUT=1m` (other settings unchanged). Run `curl -s $LAB/v1/meta`.
  4. Sign in as alice. Leave the page untouched for slightly over one minute.
  5. Sign in again as alice and, about 40 seconds in, press a key or click on the page. Leave it again.
- **Expected:**
  - Step 2: the browser goes to the IdP's logout page and returns to LabRat's Sign in page. The next sign-in asks for a username and password (or the IdP's account chooser); it does not sign alice, or the previous person, straight back in.
  - Step 3: `/v1/meta` shows `"env":"prod"`, `"idle_timeout_seconds":60` and `"oidc":{"issuer":…,"client_id":…,"scopes":…}`.
  - Step 4: after about a minute the page returns to Sign in with "Signed out after 1 minute of inactivity." Reloading does not restore the session.
  - Step 5: activity restarts the clock; sign-out comes about a minute after the last action.
  - Observation (not a failure): the timeout discards LabRat's tokens only; whether the next "Sign in with your organisation" asks for credentials after a timeout depends on the IdP's own session lifetime. Note what you saw.
- **Result:** Pass / Fail · Notes:

### UAT-15: The audit trail records everything, and the chain verifies

- **Requirements:** SPEC-006-R05, SPEC-006-R09, SPEC-006-R12, SPEC-006-R10
- **As:** QA / validation staff
- **Steps:**
  1. As carol in the UI, open Audit trail and click "Verify chain".
  2. Page through the table, or list the events from the terminal (the UI filter needs both an entity type and an id): `api $TOKEN_CAROL GET '/v1/audit/events?after=0&limit=500'`, or with jq: `curl -s -H "Authorization: Bearer $TOKEN_CAROL" "$LAB/v1/audit/events?after=0&limit=500" | jq -r '.[] | [.seq,.actor,.action,.reason] | @tsv'`
  3. Find the events from this session and check actor, action and reason for each: the bootstrap (UAT-03), the provisioning of alice, bob and bot (UAT-05), role grants (UAT-05, UAT-11), the revoke (UAT-11), the delegation (UAT-13), its end (UAT-13), and the disabling of dave (UAT-09).
  4. Check that the refused attempts (alice's grant, bob's node and the unsigned tokens) appear nowhere in the list.
  5. Run `api $TOKEN_CAROL GET /v1/audit/verify`.
- **Expected:**
  - Step 1: "Chain valid", Engine head and Database head with the same seq and hash, "Engine and database agree".
  - Step 3: events exist and carry these actors and reasons:
    - `principal.created` and `role.granted` for carol, actor `system:bootstrap-admin`, reason `bootstrap-admin`;
    - `principal.created` for alice, bob, bot, dave, actor `carol`, with the reasons typed in UAT-05 and UAT-09;
    - `role.granted` events with actor `carol` and the reasons from UAT-05 and UAT-11 (including the second grant of `reviewer` to bob);
    - `role.revoked`, actor `carol`, reason `rotated to another project`;
    - `delegation.created` and `delegation.ended`, actor `carol`, reasons from UAT-13;
    - `principal.disabled` for dave, actor `carol`, reason `visit ended`;
    - the node created by the agent shows actor `alice via bot`;
    - run signatures show actor `bob`, with the signing reason.
  - Step 4: no event exists for any refused request.
  - Step 5: the response says the chain is valid and the engine and database heads match.
  - Development-mode events from UAT-01 belong to a different tenant and do not appear here; each tenant's chain verifies by itself.
- **Result:** Pass / Fail · Notes:

### UAT-16: The database itself protects identities and role changes

- **Requirements:** SPEC-006-R04, SPEC-006-R05
- **As:** QA / validation staff (with a developer; direct database access)
- **Steps:**
  1. Open a database shell on the server's database: `docker compose exec postgres psql -U lde lde_uat006` with the local Keycloak setup (otherwise the database name in your `LDE_DATABASE_URL`, `lde` by default).
  2. As the database owner try to rename and delete a principal:
     ```sql
     UPDATE principals SET handle = 'al' WHERE handle = 'alice';
     DELETE FROM principals WHERE handle = 'alice';
     ```
  3. Try to add a second `alice` in the same tenant:
     ```sql
     INSERT INTO principals (tenant_id, kind, handle, issuer, subject)
       SELECT tenant_id, 'human', 'alice', 'https://other', 'x' FROM principals WHERE handle = 'alice';
     ```
  4. Try to grant a role without any audit event (the owner is held to this too):
     ```sql
     BEGIN;
     INSERT INTO role_grants (tenant_id, principal_id, role, reason, granted_by)
       SELECT p.tenant_id, p.id, 'qa', 'sneaky', p.id FROM principals p WHERE p.handle = 'bob';
     COMMIT;
     ```
  5. Try to erase a grant: `DELETE FROM role_grants;`
  6. Leave the session with `\q`.
- **Expected:**
  - Step 2: both fail with an error carrying the code `LDE03` (immutable). The handle is unchanged.
  - Step 3: fails with a unique-violation error (`23505`).
  - Step 4: the COMMIT fails with code `LDE02` (no matching audit event), and bob has no `qa` grant afterwards (check on the Principals tab).
  - Step 5: fails with `LDE03`.
- **Result:** Pass / Fail · Notes:

## 3. Traceability

| Requirement | Cases | Automated test |
|---|---|---|
| SPEC-006-R01 | UAT-04, UAT-07, UAT-08 | yes |
| SPEC-006-R02 | UAT-08 | yes |
| SPEC-006-R03 | UAT-04, UAT-08, UAT-09 | yes |
| SPEC-006-R04 | UAT-05, UAT-09, UAT-16 | yes |
| SPEC-006-R05 | UAT-05, UAT-06, UAT-11, UAT-15, UAT-16 | yes |
| SPEC-006-R06 | UAT-10, UAT-11 (the "reviewer changes other fields while signing" rule only by the automated test) | yes |
| SPEC-006-R07 | UAT-12, UAT-13 | yes |
| SPEC-006-R08 | UAT-13 | yes |
| SPEC-006-R09 | UAT-13, UAT-15 | yes |
| SPEC-006-R10 | UAT-01, UAT-08 (dev headers ignored in prod) | — |
| SPEC-006-R11 | UAT-02, UAT-14 | — |
| SPEC-006-R12 | UAT-03, UAT-15 | — |
| Design: sign-out also ends the IdP session | UAT-14 | — |
| Design: an admin cannot disable themselves | UAT-09 | — |
| Design: idle timeout of at least one minute | UAT-02 | — |

Out of scope and not tested here, per the spec: passwords, MFA and lockout (the IdP's job), automatic provisioning from the IdP, per-experiment permissions, and signing re-authentication (SPEC-008). The `schema_steward` role is not exercised: it exists for SPEC-010.

## 4. Sign-off

| Tester | Role | Date | Outcome | Notes |
|---|---|---|---|---|
| | | | Accepted · Accepted with issues · Rejected | |
