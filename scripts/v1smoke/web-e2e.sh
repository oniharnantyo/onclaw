#!/usr/bin/env bash
# ==============================================================================
# Web e2e: SDK-based chat through the OpenResponses /v1 surface.
#
# Boots the stub LLM + a real OnClaw server, seeds a workspace with an agent
# whose slug matches the web's mock agent id ("a-atlas"), mints an API key,
# then runs Playwright (web/tests/e2e/chat.spec.ts) against the vite dev app.
# ==============================================================================
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
export PATH="/usr/local/go/bin:$PATH"

SERVER_PORT="${SERVER_PORT:-8095}"
STUB_PORT="${STUB_PORT:-9148}"
SERVER_URL="http://127.0.0.1:${SERVER_PORT}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres@127.0.0.1:5432/onclaw_webe2e?sslmode=disable}"
SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-admin@onclaw.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD:-SmokeSuperAdminSecret123!}"
JWT_SECRET="${JWT_SECRET:-web-e2e-jwt-secret-at-least-32-chars-long!}"
ENCRYPTION_KEY="${ENCRYPTION_KEY:-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef}"

TMP_DIR=$(mktemp -d "/tmp/onclaw-webe2e.XXXXXX")
SERVER_PID=""; STUB_PID=""
cleanup() {
  local code=$?
  [[ -n "${SERVER_PID}" ]] && { kill "${SERVER_PID}" 2>/dev/null || true; wait "${SERVER_PID}" 2>/dev/null || true; }
  [[ -n "${STUB_PID}" ]] && { kill "${STUB_PID}" 2>/dev/null || true; wait "${STUB_PID}" 2>/dev/null || true; }
  rm -rf "${TMP_DIR}"
  if [[ $code -eq 0 ]]; then printf "\n🎉 web e2e passed!\n\n"; else printf "\n💥 web e2e failed!\n\n"; fi
}
trap cleanup EXIT INT TERM
mkdir -p "${TMP_DIR}/data" "${TMP_DIR}/.onclaw/workspaces/v1web/agents/a-atlas"

python3 scripts/v1smoke/stub_llm.py "${STUB_PORT}" >"${TMP_DIR}/stub.log" 2>&1 &
STUB_PID=$!
sleep 0.5

go run ./scripts/v1smoke/createdb -dsn "${DATABASE_URL}" >/dev/null
go run . migrate up --database-url "${DATABASE_URL}" >/dev/null
go build -o "${TMP_DIR}/onclaw-bin" .
DATABASE_URL="${DATABASE_URL}" \
ONCLAW_ENCRYPTION_KEY="${ENCRYPTION_KEY}" \
ONCLAW_SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL}" \
ONCLAW_SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD}" \
ONCLAW_JWT_SECRET="${JWT_SECRET}" \
ONCLAW_DATA_DIR="${TMP_DIR}/data" \
ONCLAW_DIR="${TMP_DIR}/.onclaw" \
"${TMP_DIR}/onclaw-bin" server --database-url "${DATABASE_URL}" --listen-addr "127.0.0.1:${SERVER_PORT}" --encryption-key "${ENCRYPTION_KEY}" >"${TMP_DIR}/server.log" 2>&1 &
SERVER_PID=$!
for i in {1..30}; do curl -s -f "${SERVER_URL}/healthz" >/dev/null 2>&1 && break; sleep 0.3; done
curl -s -f "${SERVER_URL}/healthz" >/dev/null || { echo "server failed to start:"; cat "${TMP_DIR}/server.log"; exit 1; }

TOKEN=$(curl -s -X POST "${SERVER_URL}/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"${SUPERADMIN_PASSWORD}\"}" | jq -r .token)
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || { echo "login failed"; exit 1; }

curl -s -X POST "${SERVER_URL}/api/v1/workspaces" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Web E2E","slug":"v1web"}' >/dev/null
PROVIDER_ID=$(curl -s -X POST "${SERVER_URL}/api/v1/workspaces/v1web/providers" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"type\":\"openai-compatible\",\"name\":\"Stub\",\"base_url\":\"http://127.0.0.1:${STUB_PORT}/v1\",\"key\":\"stub-secret-key\"}" | jq -r '.provider.id // .id')
curl -s -X POST "${SERVER_URL}/api/v1/workspaces/v1web/agents" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"a-atlas\",\"name\":\"Atlas\",\"role\":\"E2E runner\",\"brief\":\"Runs the web e2e\",\"provider_id\":\"${PROVIDER_ID}\",\"model\":\"stub-1\",\"tools\":[\"execute\"],\"autonomy\":\"approval\"}" >/dev/null
API_KEY=$(curl -s -X POST "${SERVER_URL}/api/v1/workspaces/v1web/api-keys" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"e2e"}' | jq -r .key)
[[ -n "$API_KEY" && "$API_KEY" != "null" ]] || { echo "api key mint failed"; exit 1; }

cd web
ONCLAW_SERVER_URL="${SERVER_URL}" ONCLAW_API_KEY="${API_KEY}" \
ONCLAW_EMAIL="${SUPERADMIN_EMAIL}" ONCLAW_PASSWORD="${SUPERADMIN_PASSWORD}" \
E2E_BASE_URL="http://localhost:5173" VITE_API_PROXY_TARGET="${SERVER_URL}" \
pnpm exec playwright test tests/e2e/chat.spec.ts --reporter=line
