# Local Keycloak for prod-mode testing

A throwaway identity provider so you can run LabRat with `LDE_ENV=prod` on your machine, and walk the SPEC-006 UAT plan. **Test only**: fixed passwords, no TLS, and the realm is re-imported on every start, so nothing is kept.

```bash
docker compose --profile idp up -d keycloak      # http://localhost:8081 (console: admin / admin)
```

The realm `lab` (`lab-realm.json`) has:

| Login | Password | Keycloak id (the token's `sub`) | `lde_tenant` | UAT role |
|---|---|---|---|---|
| `alice` | `labrat` | `11111111-1111-4111-8111-000000000001` | T1 | scientist |
| `bob` | `labrat` | `11111111-1111-4111-8111-000000000002` | T1 | reviewer |
| `carol` | `labrat` | `11111111-1111-4111-8111-000000000003` | T1 | admin (bootstrap her) |
| `dave` | `labrat` | `11111111-1111-4111-8111-000000000004` | T1 | none: never provisioned |
| `mallory` | `labrat` | `11111111-1111-4111-8111-000000000005` | T2 | another tenant |
| client `labrat-bot`, secret `bot-secret` | | `11111111-1111-4111-8111-000000000006` | T1 | agent |

T1 is `00000000-0000-4000-8000-0000000000a1`, and T2 is `00000000-0000-4000-8000-0000000000b2`. The UI client is `labrat-ui` (public, PKCE). It redirects to `http://localhost:8080/` only, so run LabRat on port 8080. Tokens carry `aud: labrat` and last 15 minutes.

## Run LabRat against it

Use a database of its own: these migrations make older branches' writes fail.

```bash
docker exec labrat-postgres-1 psql -U lde -d lde -c "CREATE DATABASE lde_uat006 OWNER lde"   # once

export LDE_DATABASE_URL="postgres://lde:lde@localhost:5432/lde_uat006?sslmode=disable"
go run ./cmd/server bootstrap-admin --tenant 00000000-0000-4000-8000-0000000000a1 --slug lab-a1 --name "UAT lab" \
  --issuer http://localhost:8081/realms/lab --subject 11111111-1111-4111-8111-000000000003 \
  --handle carol --display-name "Carol Admin" --email carol@lab.test                        # once per database

export LDE_ENV=prod
export LDE_OIDC_ISSUER=http://localhost:8081/realms/lab
export LDE_OIDC_AUDIENCE=labrat
export LDE_OIDC_UI_CLIENT_ID=labrat-ui
go run ./cmd/server
```

Open http://localhost:8080/, choose **Sign in with your organisation**, and log in as `carol`. Under **Principals** she can create alice, bob and the agent bot. Use issuer `http://localhost:8081/realms/lab` and the subjects in the table.

An agent token for API calls:

```bash
curl -s http://localhost:8081/realms/lab/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=labrat-bot -d client_secret=bot-secret | jq -r .access_token
```

There's no password grant, so human tokens come only from the UI's sign-in. For API checks as a human, copy the access token from the browser's Network tab: it's in the `/token` response.
