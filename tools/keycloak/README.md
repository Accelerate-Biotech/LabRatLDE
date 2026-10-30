# Local Keycloak for prod-mode testing

A throwaway identity provider so you can run LabRat with `LDE_ENV=prod` on your machine, and walk the SPEC-006 UAT plan. **Test only**: fixed passwords, a password-grant client, no TLS. The realm is imported when the container is created; a plain restart keeps what's there.

## Start over (fresh UAT)

Stop your LabRat server first, then:

```bash
sh tools/keycloak/reset-uat.sh          # fresh Keycloak realm, empty lde_uat006 database, UI built if missing
sh tools/keycloak/reset-uat.sh --all    # the above, plus deletes EVERY local LabRat database (asks you to type "nuke")
```

The database is left empty on purpose: UAT-03 creates the first admin. Afterwards, sign in from a **new private window**, so no old Keycloak session cookie is reused.

Then, in every terminal:

```bash
. tools/keycloak/uat.env
```

This sets `LDE_DATABASE_URL` (to `lde_uat006`), `LAB`, `T1`, `T2`, `ISSUER` and `SUB_*`, and adds three commands:

| Command | Does |
|---|---|
| `tokens` | exports `TOKEN_ALICE`, `TOKEN_BOB`, `TOKEN_CAROL`, `TOKEN_DAVE`, `TOKEN_MALLORY` and `TOKEN_BOT`; rerun it when a token expires (15 min) |
| `tok <user>` | prints one fresh token |
| `prod_env` | exports the prod server settings (`LDE_ENV=prod` and the OIDC variables) |

Other commands: `docker compose --profile idp up -d keycloak` starts Keycloak on http://localhost:8081 (console `admin` / `admin`), and `docker compose stop keycloak` stops it.

## The realm

The realm `lab` (`lab-realm.json`) has:

| Login | Password | Keycloak id (the token's `sub`) | `lde_tenant` | UAT role |
|---|---|---|---|---|
| `alice` | `labrat` | `11111111-1111-4111-8111-000000000001` | T1 | scientist |
| `bob` | `labrat` | `11111111-1111-4111-8111-000000000002` | T1 | reviewer |
| `carol` | `labrat` | `11111111-1111-4111-8111-000000000003` | T1 | admin (bootstrapped in UAT-03) |
| `dave` | `labrat` | `11111111-1111-4111-8111-000000000004` | T1 | none: never provisioned |
| `mallory` | `labrat` | `11111111-1111-4111-8111-000000000005` | T2 | another tenant |
| client `labrat-bot`, secret `bot-secret` | | `11111111-1111-4111-8111-000000000006` | T1 | agent |

T1 is `00000000-0000-4000-8000-0000000000a1`, and T2 is `00000000-0000-4000-8000-0000000000b2`. Tokens carry `aud: labrat` and last 15 minutes.

Clients:
- `labrat-ui`: public, PKCE. It redirects to `http://localhost:8080/` only, so run LabRat on port 8080.
- `labrat-uat`: public, password grant. It's used only by `tok`/`tokens` for terminal steps. Never configure a real IdP this way.
- `labrat-bot`: client credentials.

## Without the UAT plan

To get a working prod setup in one go:

```bash
sh tools/keycloak/reset-uat.sh && . tools/keycloak/uat.env
go run ./cmd/server bootstrap-admin --tenant $T1 --slug lab-a1 --name "UAT lab" \
  --issuer $ISSUER --subject $SUB_CAROL --handle carol --display-name "Carol Admin" --email carol@lab.test
prod_env && go run ./cmd/server
```

Then open http://localhost:8080/, choose **Sign in with your organisation**, and log in as `carol`. Under **Principals** she can create alice, bob and the agent bot, with issuer `$ISSUER` and the subjects in the table.
