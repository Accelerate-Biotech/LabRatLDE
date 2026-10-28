#!/bin/sh
# Resets the local prod-mode UAT environment to a clean start:
#   1. recreates the Keycloak container, so the lab realm is re-imported
#      (users, clients, sessions and anything changed in the console are lost);
#   2. drops and recreates the lde_uat006 database;
#   3. bootstraps carol as the tenant's admin.
# Run from the repository root, with your LabRat server stopped:
#   sh tools/keycloak/reset-uat.sh
set -e

DB=lde_uat006
ISSUER=http://localhost:8081/realms/lab

if curl -s -o /dev/null http://localhost:8080/healthz; then
  echo "A server is still running on :8080. Stop it (Ctrl+C), then run this again." >&2
  exit 1
fi

echo "== Keycloak: recreating the container (re-imports the lab realm)"
docker compose --profile idp rm -sf keycloak
docker compose --profile idp up -d keycloak
curl -s -o /dev/null --retry 90 --retry-all-errors --retry-delay 2 --fail "$ISSUER/.well-known/openid-configuration"
echo "   ready at $ISSUER"

echo "== Postgres: dropping and recreating $DB"
docker compose exec -T postgres psql -U lde -d lde -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB OWNER lde"

echo "== LabRat: migrating and bootstrapping carol as admin"
LDE_ENV=dev LDE_DATABASE_URL="postgres://lde:lde@localhost:5432/$DB?sslmode=disable" \
  go run ./cmd/server bootstrap-admin --tenant 00000000-0000-4000-8000-0000000000a1 --slug lab-a1 --name "UAT lab" \
  --issuer "$ISSUER" --subject 11111111-1111-4111-8111-000000000003 \
  --handle carol --display-name "Carol Admin" --email carol@lab.test 2>&1 | grep -v 'level=INFO' || true

cat <<EOF

Fresh. Now start LabRat:

  export LDE_DATABASE_URL="postgres://lde:lde@localhost:5432/$DB?sslmode=disable"
  export LDE_ENV=prod LDE_OIDC_ISSUER=$ISSUER LDE_OIDC_AUDIENCE=labrat LDE_OIDC_UI_CLIENT_ID=labrat-ui
  go run ./cmd/server

then open http://localhost:8080/ in a new private window and sign in as carol / labrat.
EOF
