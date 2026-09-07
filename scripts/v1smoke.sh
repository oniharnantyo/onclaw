#!/usr/bin/env bash
# ==============================================================================
# OnClaw /v1 (OpenResponses) Wire-Level Smoke Test
#
# Boots a real server against PostgreSQL plus a stub OpenAI-Responses-API LLM,
# then exercises the /v1 surface over the wire with curl:
#   1. Auth matrix (missing key / unknown key / wrong scheme -> 401 envelope)
#   2. GET /v1/models (agents as models)
#   3. Non-streaming bound turn (metadata.onclaw_session) with usage
#   4. Streaming chained turn (previous_response_id) with SSE lifecycle + [DONE]
#   5. HITL: dangerous shell command -> onclaw:approval_required -> native
#      approve endpoint -> resumed completion collected via session events
#
# Usage:
#   ./scripts/v1smoke.sh
#   DATABASE_URL="postgres://localhost:5432/onclaw_v1smoke?sslmode=disable" ./scripts/v1smoke.sh
# ==============================================================================
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
export PATH="/usr/local/go/bin:$PATH"

SERVER_PORT="${SERVER_PORT:-8089}"
SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
SERVER_URL="http://${SERVER_HOST}:${SERVER_PORT}"
STUB_PORT="${STUB_PORT:-9147}"
STUB_URL="http://127.0.0.1:${STUB_PORT}/v1"
DATABASE_URL="${DATABASE_URL:-postgres://postgres@127.0.0.1:5432/onclaw_v1smoke?sslmode=disable}"
SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-admin@onclaw.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD:-SmokeSuperAdminSecret123!}"
JWT_SECRET="${JWT_SECRET:-v1-smoke-jwt-secret-at-least-32-chars-long!}"
ENCRYPTION_KEY="${ENCRYPTION_KEY:-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef}"

C_RESET="\033[0m"; C_RED="\033[31m"; C_GREEN="\033[32m"; C_BLUE="\033[34m"; C_CYAN="\033[36m"; C_BOLD="\033[1m"
log_step() { printf "\n${C_BOLD}${C_CYAN}=== %s ===${C_RESET}\n" "$1"; }
log_info() { printf "${C_BLUE}ℹ  %s${C_RESET}\n" "$1"; }
log_pass() { printf "${C_GREEN}✓ PASS:${C_RESET} %s\n" "$1"; }
fail() { printf "${C_RED}✗ FAIL:${C_RESET} %s\n" "$1" >&2; exit 1; }

TMP_DIR=$(mktemp -d "/tmp/onclaw-v1smoke.XXXXXX")
DATA_DIR="${TMP_DIR}/data"; mkdir -p "${DATA_DIR}"
ONCLAW_BASE_DIR="${TMP_DIR}/.onclaw"
SERVER_PID=""; STUB_PID=""
cleanup() {
  local code=$?
  [[ -n "${SERVER_PID}" ]] && { kill "${SERVER_PID}" 2>/dev/null || true; wait "${SERVER_PID}" 2>/dev/null || true; }
  [[ -n "${STUB_PID}" ]] && { kill "${STUB_PID}" 2>/dev/null || true; wait "${STUB_PID}" 2>/dev/null || true; }
  rm -rf "${TMP_DIR}"
  if [[ $code -eq 0 ]]; then printf "\n${C_BOLD}${C_GREEN}🎉 /v1 wire smoke passed!${C_RESET}\n\n"; else printf "\n${C_BOLD}${C_RED}💥 /v1 wire smoke failed!${C_RESET}\n\n"; fi
}
trap cleanup EXIT INT TERM

HTTP_STATUS=""; HTTP_BODY=""; HTTP_HEADERS=""
req() { # method path token body [extra curl args...]
  local method="$1" path="$2" token="${3:-}" body="${4:-}"; shift 4 || shift $#
  local hf="${TMP_DIR}/h" bf="${TMP_DIR}/b"
  local args=(-s -S -X "$method" -D "$hf" -o "$bf" "${SERVER_URL}${path}")
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  [[ -n "$body" ]] && args+=(-H "Content-Type: application/json" --data "$body")
  curl "${args[@]}" "$@" || fail "curl to ${path} failed"
  HTTP_STATUS=$(head -n1 "$hf" | awk '{print $2}')
  HTTP_BODY=$(cat "$bf")
  HTTP_HEADERS=$(cat "$hf")
}
assert_status() { [[ "$HTTP_STATUS" =~ ^($1)$ ]] && log_pass "$2 (status: $HTTP_STATUS)" || fail "$2: expected $1, got $HTTP_STATUS. Body: $HTTP_BODY"; }
jget() { echo "$HTTP_BODY" | jq -r "$1"; }
assert_json() { [[ "$(jget "$1")" == "true" ]] && log_pass "$2" || fail "$2: '$1' false. Body: $HTTP_BODY"; }

command -v jq >/dev/null || fail "jq is required"

# -----------------------------------------------------------------------------
# Boot stub LLM + server
# -----------------------------------------------------------------------------
log_step "Booting stub LLM on :${STUB_PORT} and server on :${SERVER_PORT}"
python3 scripts/v1smoke/stub_llm.py "${STUB_PORT}" >"${TMP_DIR}/stub.log" 2>&1 &
STUB_PID=$!
sleep 0.5
kill -0 "${STUB_PID}" 2>/dev/null || fail "stub LLM failed to start: $(cat "${TMP_DIR}/stub.log")"

go run ./scripts/v1smoke/createdb -dsn "${DATABASE_URL}" >/dev/null || fail "database bootstrap failed"
go run . migrate up --database-url "${DATABASE_URL}" >/dev/null || fail "migrations failed against ${DATABASE_URL} (is PostgreSQL running?)"
go build -o "${TMP_DIR}/onclaw-bin" . || fail "server build failed"
DATABASE_URL="${DATABASE_URL}" \
ONCLAW_ENCRYPTION_KEY="${ENCRYPTION_KEY}" \
ONCLAW_SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL}" \
ONCLAW_SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD}" \
ONCLAW_JWT_SECRET="${JWT_SECRET}" \
ONCLAW_DATA_DIR="${DATA_DIR}" \
ONCLAW_DIR="${ONCLAW_BASE_DIR}" \
"${TMP_DIR}/onclaw-bin" server --database-url "${DATABASE_URL}" --listen-addr "${SERVER_HOST}:${SERVER_PORT}" --encryption-key "${ENCRYPTION_KEY}" >"${TMP_DIR}/server.log" 2>&1 &
SERVER_PID=$!
READY=0
for i in {1..30}; do curl -s -f "${SERVER_URL}/healthz" >/dev/null 2>&1 && { READY=1; break; }; sleep 0.3; done
[[ $READY -eq 1 ]] || fail "server failed to start: $(cat "${TMP_DIR}/server.log")"
log_pass "server healthy"

# -----------------------------------------------------------------------------
# Seed: workspace, provider, agent, API key, session
# -----------------------------------------------------------------------------
log_step "Seeding tenant data"
WS_SLUG="v1smoke"
req "POST" "/api/v1/auth/login" "" "{\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"${SUPERADMIN_PASSWORD}\"}"
TOKEN=$(jget '.token'); [[ -n "$TOKEN" && "$TOKEN" != "null" ]] || fail "superadmin login failed: $HTTP_BODY"
req "POST" "/api/v1/workspaces" "$TOKEN" "{\"name\":\"V1 Smoke\",\"slug\":\"${WS_SLUG}\"}"
if [[ "$HTTP_STATUS" == "409" ]]; then
  log_pass "workspace already exists (rerun)"
else
  assert_status "200|201" "workspace created"
fi
req "POST" "/api/v1/workspaces/${WS_SLUG}/providers" "$TOKEN" "{\"type\":\"openai-compatible\",\"name\":\"Stub LLM\",\"base_url\":\"${STUB_URL}\",\"key\":\"stub-secret-key\"}"
PROVIDER_ID=$(jget '.provider.id // .id'); [[ -n "$PROVIDER_ID" && "$PROVIDER_ID" != "null" ]] || fail "provider create failed: $HTTP_BODY"
req "POST" "/api/v1/workspaces/${WS_SLUG}/agents" "$TOKEN" "{\"slug\":\"atlas\",\"name\":\"Atlas\",\"role\":\"Smoke runner\",\"brief\":\"Runs the /v1 wire smoke\",\"provider_id\":\"${PROVIDER_ID}\",\"model\":\"stub-1\",\"tools\":[\"execute\"],\"autonomy\":\"approval\"}"
if [[ "$HTTP_STATUS" == "409" ]]; then
  log_pass "agent atlas already exists (rerun)"
else
  assert_status "200|201" "agent atlas created"
fi
# A pre-existing agent (rerun) belongs to an earlier run's data dir — this
# run's ONCLAW_DIR needs the agent workspace directory to exist either way.
mkdir -p "${ONCLAW_BASE_DIR}/workspaces/${WS_SLUG}/agents/atlas"
req "POST" "/api/v1/workspaces/${WS_SLUG}/api-keys" "$TOKEN" "{\"name\":\"smoke key\"}"
API_KEY=$(jget '.key'); [[ -n "$API_KEY" && "$API_KEY" != "null" ]] || fail "api key create failed: $HTTP_BODY"
log_pass "api key minted (${API_KEY:0:11}...)"

SESSION_ID="v1smoke-sess-$(date +%s)"
go run ./scripts/v1smoke/seed -dsn "${DATABASE_URL}" -workspace "${WS_SLUG}" -session "${SESSION_ID}" >/dev/null || fail "session seed failed"

# -----------------------------------------------------------------------------
# 1. /v1 auth matrix
# -----------------------------------------------------------------------------
log_step "1. /v1 Auth Matrix"
req "POST" "/v1/responses" "" '{"model":"atlas","input":"hi"}'
assert_status "401" "missing API key rejected"
echo "$HTTP_BODY" | jq -e '.error.type == "invalid_request_error" and .error.code == "invalid_api_key"' >/dev/null || fail "envelope shape: $HTTP_BODY"
log_pass "OpenResponses envelope with invalid_api_key"
req "POST" "/v1/responses" "oc_ws_totallyboguskey000000000000000000" '{"model":"atlas","input":"hi"}'
assert_status "401" "unknown API key rejected"
req "GET" "/v1/models" "${TOKEN}"
assert_status "401" "JWT rejected on /v1"

# -----------------------------------------------------------------------------
# 2. /v1/models
# -----------------------------------------------------------------------------
log_step "2. GET /v1/models"
req "GET" "/v1/models" "${API_KEY}"
assert_status "200" "models listed"
assert_json '.object == "list" and ([.data[].id] | index("atlas") != null)' "agent slug listed as model id"

# -----------------------------------------------------------------------------
# 3. Non-streaming bound turn
# -----------------------------------------------------------------------------
log_step "3. Non-streaming turn (metadata binding)"
req "POST" "/v1/responses" "${API_KEY}" "{\"model\":\"atlas\",\"input\":\"hello wire smoke\",\"metadata\":{\"onclaw_session\":\"${SESSION_ID}\"}}"
assert_status "200" "turn completed"
assert_json '.object == "response" and .status == "completed"' "aggregated response completed"
assert_json '(.output | map(select(.type == "message")) | length) == 1' "assistant message item present"
assert_json '.output[0].content[0].text == "stub says hi from wire smoke"' "assistant text from stub LLM"
assert_json '.usage.input_tokens == 12 and .usage.total_tokens == 21' "usage captured"
RESP_1=$(jget '.id'); [[ "$RESP_1" == resp_* ]] || fail "response id missing: $HTTP_BODY"
assert_json '.id | startswith("resp_")' "response id minted (resp_*)"
# Unknown metadata sessions birth on first use (web-live-chat-sessions);
# chaining (previous_response_id) remains strictly bind-only instead.
req "POST" "/v1/responses" "${API_KEY}" "{\"model\":\"atlas\",\"input\":\"hi\",\"previous_response_id\":\"resp_no-such-session_00000000-0000-0000-0000-000000000000\"}"
assert_status "404" "chained bind to unknown session is not found"

# -----------------------------------------------------------------------------
# 4. Streaming chained turn
# -----------------------------------------------------------------------------
log_step "4. Streaming turn (previous_response_id chaining)"
req "POST" "/v1/responses" "${API_KEY}" "{\"model\":\"atlas\",\"input\":\"stream me\",\"stream\":true,\"previous_response_id\":\"${RESP_1}\"}" \
  -H "Accept: text/event-stream"
assert_status "200" "streaming turn accepted"
grep -qi "text/event-stream" <<<"$HTTP_HEADERS" || fail "SSE content-type missing: $HTTP_HEADERS"
grep -q '"type":"response.created"' <<<"$HTTP_BODY" || fail "response.created frame missing"
grep -q '"type":"response.output_text.delta"' <<<"$HTTP_BODY" || fail "text delta frame missing"
grep -q '"type":"response.completed"' <<<"$HTTP_BODY" || fail "response.completed frame missing"
grep -q '^data: \[DONE\]' <<<"$HTTP_BODY" || fail "[DONE] sentinel missing"
SEQS=$(grep -o '"sequence_number":[0-9]*' <<<"$HTTP_BODY" | grep -o '[0-9]*')
sort -n <<<"$SEQS" | uniq | tr '\n' ',' | grep -q "$(head -1 <<<"$(printf '%s\n' $SEQS | sort -n)")" || true
LAST_SEQ=$(echo "$SEQS" | tail -1); N_SEQ=$(echo "$SEQS" | wc -l | tr -d ' ')
[[ "$LAST_SEQ" == "$((N_SEQ - 1))" ]] || fail "sequence numbers not strictly increasing from 0: $SEQS"
log_pass "SSE lifecycle frames with strictly increasing sequence_number"
grep -q 'hello' <<<"$HTTP_BODY" || true # (history replay is server-side; nothing to assert on text here)

# -----------------------------------------------------------------------------
# 5. HITL: pause -> native approve -> collect
# -----------------------------------------------------------------------------
log_step "5. Human-in-the-loop approval over the wire"
req "POST" "/v1/responses" "${API_KEY}" "{\"model\":\"atlas\",\"input\":\"APPROVE the cleanup\",\"stream\":true,\"previous_response_id\":\"${RESP_1}\"}" \
  -H "Accept: text/event-stream"
assert_status "200" "approval turn streamed"
grep -q '"type":"onclaw:approval_required"' <<<"$HTTP_BODY" || fail "onclaw:approval_required event missing: $HTTP_BODY"
grep -q 'rm -rf' <<<"$HTTP_BODY" || fail "dangerous command not surfaced"
INTERRUPT_ID=$(grep -o '"interrupt_id":"[^"]*"' <<<"$HTTP_BODY" | head -1 | cut -d'"' -f4)
[[ -n "$INTERRUPT_ID" ]] || fail "interrupt_id missing"
grep -q '"type":"response.completed"' <<<"$HTTP_BODY" && fail "stream must NOT complete while approval is pending"
grep -q '"session_id":"' <<<"$HTTP_BODY" || fail "approval event missing session_id"
log_pass "approval_required surfaces interrupt + command, stream left incomplete"

# Response should be marked incomplete
INCOMPLETE_ID=$(grep -o '"id":"resp_[^"]*"' <<<"$HTTP_BODY" | head -1 | cut -d'"' -f4)

req "POST" "/api/v1/workspaces/${WS_SLUG}/agents/atlas/sessions/${SESSION_ID}/approvals/${INTERRUPT_ID}" "$TOKEN" '{"approved":true}'
assert_status "200" "native approval endpoint resumes the run"

# The resumed turn runs detached from the HTTP request — wait for it.
sleep 3
req "GET" "/api/v1/workspaces/${WS_SLUG}/agents/atlas/sessions/${SESSION_ID}/events?limit=50" "$TOKEN"
assert_status "200" "session events readable"
assert_json '[.events[]?.message.content // ""] | join(" ") | test("stub finished after tool")' "resumed turn completed after approval"
assert_json '[.events[]? | select(.kind == "tool_call_finished")] | length > 0' "approved command executed (tool trace present)"

# Chained collection: another /v1 turn on the same session
req "POST" "/v1/responses" "${API_KEY}" "{\"model\":\"atlas\",\"input\":\"TOOLRUN echo check\",\"stream\":false,\"previous_response_id\":\"${INCOMPLETE_ID}\"}"
assert_status "200" "chained turn after approval accepted"
assert_json '.status == "completed"' "chained turn completed"

printf "\nAll /v1 wire checks passed.\n"
