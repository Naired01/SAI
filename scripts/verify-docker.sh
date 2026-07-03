#!/usr/bin/env bash
# scripts/verify-docker.sh
# Verifica end-to-end el ciclo Docker del server SAI:
#   build imagen local, levanta postgres + server, bootstrap admin,
#   compila smoketest local y lo corre contra el server.
#
# Uso: ./scripts/verify-docker.sh
set -euo pipefail

cd "$(dirname "$0")/.."

ADMIN_EMAIL="${SAI_ADMIN_EMAIL:-admin@sai.local}"
ADMIN_PASSWORD="${SAI_ADMIN_PASSWORD:-Test#2026}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"

echo "=== SAI Docker verify ==="
echo "Admin:    $ADMIN_EMAIL"
echo "Password: $ADMIN_PASSWORD"
echo

# 1. Build
echo "[1/6] docker compose build server"
docker compose build server 2>&1 | tail -5

# 2. Up (sin bootstrap; solo postgres + server)
echo "[2/6] docker compose up -d"
docker compose up -d postgres server

# 3. Esperar a que el server responda /health
echo "[3/6] Esperando /api/v1/health (timeout ${HEALTH_TIMEOUT}s)…"
for i in $(seq 1 "$HEALTH_TIMEOUT"); do
  if docker compose exec -T server /app/sai-server --healthcheck 2>/dev/null; then
    echo "      healthcheck OK (t=${i}s)"
    HEALTHY=1
    break
  fi
  sleep 1
done
if [ "${HEALTHY:-0}" != "1" ]; then
  echo "      healthcheck FAILED tras ${HEALTH_TIMEOUT}s"
  echo
  echo "      Logs del server:"
  docker compose logs --tail=80 server
  exit 1
fi

# 4. Bootstrap admin
echo "[4/6] docker compose exec server --bootstrap"
docker compose exec -T server /app/sai-server --bootstrap \
    --admin-email "$ADMIN_EMAIL" \
    --admin-password "$ADMIN_PASSWORD" 2>&1 | tail -5

# 5. Compilar smoketest
echo "[5/6] go build ./cmd/smoketest"
go build -o bin/smoketest ./cmd/smoketest

# 6. Smoke test
echo "[6/6] cmd/smoketest contra el server"
SAI_URL="http://localhost:8080" \
SAI_ADMIN_EMAIL="$ADMIN_EMAIL" \
SAI_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
./bin/smoketest

echo
echo "=== verify OK ==="