#!/bin/sh
# Wipes the local SPEC-006 UAT environment so the plan starts from nothing:
#   - recreates the Keycloak container (lab realm re-imported, all sessions gone);
#   - drops and recreates the lde_uat006 database (empty: UAT-03 creates the first admin);
#   - builds the web UI if web/dist has no build yet.
#
#   sh tools/keycloak/reset-uat.sh         # the UAT database and Keycloak only
#   sh tools/keycloak/reset-uat.sh --all   # also deletes the Postgres volume: EVERY local LabRat
#                                          # database (lde, lde_spec006, ...) is lost
#
# Stop any LabRat server on :8080 first. Afterwards use a new private browser window.
set -e
cd "$(dirname "$0")/../.."

DB=lde_uat006
ISSUER=http://localhost:8081/realms/lab
ALL=no
case "${1:-}" in
  "") ;;
  --all) ALL=yes ;;
  *) echo "usage: sh tools/keycloak/reset-uat.sh [--all]" >&2; exit 2 ;;
esac

if curl -s -o /dev/null http://localhost:8080/healthz; then
  echo "A server is still running on :8080. Stop it (Ctrl+C), then run this again." >&2
  exit 1
fi

if [ "$ALL" = yes ]; then
  echo "This deletes the labrat Postgres volume: every local LabRat database and its data, for every branch."
  printf 'Type nuke to continue: '
  read -r answer
  [ "$answer" = nuke ] || { echo "Cancelled; nothing changed."; exit 1; }
  echo "== Removing all LabRat containers and volumes"
  docker compose --profile idp --profile full down -v --remove-orphans
  docker compose up -d --wait postgres
else
  echo "== Keycloak: removing the container"
  docker compose --profile idp rm -sf keycloak
  docker compose up -d --wait postgres
fi

echo "== Keycloak: starting fresh (re-imports the lab realm)"
docker compose --profile idp up -d keycloak
curl -s -o /dev/null --retry 90 --retry-all-errors --retry-delay 2 --fail "$ISSUER/.well-known/openid-configuration"
echo "   ready at $ISSUER"

echo "== Postgres: dropping and recreating $DB (empty)"
docker compose exec -T postgres psql -U lde -d lde -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB OWNER lde"

if [ ! -f web/dist/index.html ]; then
  echo "== Web UI: building (web/dist was empty)"
  (cd web && npm ci --no-audit --no-fund && npm run build) >/dev/null
fi

cat <<'EOF'

Fresh. In each terminal you use for the UAT:

  . tools/keycloak/uat.env      # sets LDE_DATABASE_URL, LAB, T1, T2, ISSUER, SUB_*; adds tok, tokens, prod_env

Part A (dev):   go run ./cmd/server
Part B (prod):  prod_env && go run ./cmd/server     then, in the helper terminal: tokens

Open http://localhost:8080/ in a NEW private window. The tenant has no principals yet;
UAT-03 creates carol with bootstrap-admin --issuer "$ISSUER" --subject "$SUB_CAROL".
EOF
