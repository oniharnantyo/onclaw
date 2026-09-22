#!/usr/bin/env bash
# ==============================================================================
# OnClaw End-to-End Smoke Test Suite
#
# Tests the full backend lifecycle via HTTP API:
#   1. Server health check
#   2. Env-seeded superadmin login & auth failure modes
#   3. Auth /me verification
#   4. Admin master-tenant protection & tenant creation
#   5. Workspace suspend/restore lifecycle & suspended guards
#   6. User provisioning & credential authentication
#   7. Workspace member management & permission algebra guards
#   8. Multi-tenant enumeration defense (404-over-403)
#   9. Avatar image upload, sniffing, and capability URL public serving
#  10. Last-owner & last-superadmin protection guards
#  11. CLI user provisioning & authentication verification
#  12. Workspace provider configuration CRUD & verify scenarios
#  13. Agents, skills, tools, user & workspace memory endpoints, birth flow
#  14. /v1 OpenResponses live chat sessions (chat key exchange → birth → chain)
#  15. MCP server registry & agent-private servers (CRUD, probes, enabled_mcps)
#  16. Agent hooks: workspace CRUD, validation, reorder, dry-run, executions,
#      agent-level hooks, instance-admin managed hooks
#  17. Channels: CRUD, membership, feed & SSE (integrate-agent-channels)
#  18. Teams: templates, materialization, kickoff & work sessions
#      (channel-teams: spawn-all materialization, in-session hop chains,
#      human gates, facilitator close via session.close)
#  19. /v1 compact command (chat-compact-command: bind-only compaction turn,
#      onclaw:context_compacted frame, completed-with-usage)
#  20. Durable agent session index (agent-session-index: /v1 turn births the
#      index row with the derived title, per-user private listing, running
#      flag, foreign-delete 404, soft delete hides the row)
#  21. Chat attachments (workspace-attachments: upload + lane validation,
#      capability-URL serving with byte equality, oversize/office rejection,
#      workspace storage config API with member gating — local driver)
#  22. Schedulers (integrate-scheduler: permission guards, run-now with a
#      real agent run + transcript, channel delivery, pause flow)
#  23. Telegram gateway (integrate-telegram-gateway: auth gates, write-only
#      bot token with secret hint, config PUT/GET roundtrip, enable/disable,
#      group bindings with conflict 409/422, pairing token lifecycle, webhook
#      secret enforcement)
#      + Langfuse link-out contract (integrate-langfuse-tracing 5.2: run
#      payloads expose langfuse_url null on unconfigured instances)
#  24. WhatsApp gateway (add-whatsapp-gateway: lane validation, write-only
#      cloud credential envelope with no echo, Meta verification handshake
#      GET, X-Hub-Signature-256 rejection, message → run → outbox delivery
#      through a stubbed Meta endpoint, pairing-token lifecycle, multi-device
#      pairing surface without WhatsApp connectivity)
#  25. Agent heartbeat (add-agent-heartbeat: create with the seeded default
#      checklist, run-now silence contract (NO_REPLY → suppressed), channel
#      delivery of a report, active-hours skip, five-failure auto-pause and
#      resume — via a dedicated steerable mock provider)
#  26. Extracted memory (integrate-agent-zero-memory: notes/events listings,
#      provider-pinned settings record, connection-test failure
#      shapes, promote/tombstone 404 paths, empty morning report)
#  27. Workspace files API (add-right-panel: agent jail file read/list with
#      path confinement — traversal and absolute paths rejected as not found,
#      attachment disposition for html/svg, nosniff on reads and listings)
#  28. Workspace service connections (add-workspace-connections: recipe
#      gallery with availability, integrations.write guards, connect
#      validation envelopes, the real-upstream probe gate storing nothing on
#      failure, and unknown-id 404 paths)
#
# Usage:
#   ./scripts/smoke.sh
#   DATABASE_URL="postgres://user:pass@localhost:5432/onclaw?sslmode=disable" ./scripts/smoke.sh
#   SERVER_URL="http://localhost:8080" ./scripts/smoke.sh
# ==============================================================================

set -euo pipefail

# Anchor to the repo root so the `go run` / `go build` calls below work
# regardless of the caller's working directory.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Configuration with sensible defaults
SERVER_PORT="${SERVER_PORT:-8088}"
SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
SERVER_URL="${SERVER_URL:-http://${SERVER_HOST}:${SERVER_PORT}}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres@127.0.0.1:5432/onclaw_smoke?sslmode=disable}"
SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-admin@onclaw.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD:-SmokeSuperAdminSecret123!}"
JWT_SECRET="${JWT_SECRET:-smoke-test-jwt-secret-at-least-32-chars-long!}"
ENCRYPTION_KEY="${ONCLAW_ENCRYPTION_KEY:-$(openssl rand -hex 32 2>/dev/null || echo "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")}"

# WhatsApp Cloud API stub (section 24, add-whatsapp-gateway): the server's
# cloud-lane adapters talk to this stub instead of graph.facebook.com via
# ONCLAW_WHATSAPP_CLOUD_API_BASE. When the server is started by this script
# the stub port is picked here (it must ride the server's env at boot); when
# an external server is reused, its ONCLAW_WHATSAPP_CLOUD_API_BASE is honored
# and the stub binds to that port. The stub itself launches in section 24.
if [[ -n "${ONCLAW_WHATSAPP_CLOUD_API_BASE:-}" ]]; then
    WA_STUB_BASE="${ONCLAW_WHATSAPP_CLOUD_API_BASE}"
    WA_STUB_PORT="${WA_STUB_BASE##*:}"
else
    WA_STUB_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
    WA_STUB_BASE="http://127.0.0.1:${WA_STUB_PORT}"
fi
WA_STUB_PID=""
HB_MOCK_PID=""
MOCK_PID=""
SERVER_PID=""

# Colors
C_RESET="\033[0m"
C_RED="\033[31m"
C_GREEN="\033[32m"
C_YELLOW="\033[33m"
C_BLUE="\033[34m"
C_CYAN="\033[36m"
C_BOLD="\033[1m"

log_info() {
    printf "${C_BLUE}ℹ  %s${C_RESET}\n" "$1"
}

log_step() {
    printf "\n${C_BOLD}${C_CYAN}=== %s ===${C_RESET}\n" "$1"
}

log_pass() {
    printf "${C_GREEN}✓ PASS:${C_RESET} %s\n" "$1"
}

log_fail() {
    printf "${C_RED}✗ FAIL:${C_RESET} %s\n" "$1" >&2
    if [[ -f "${TMP_DIR}/server.log" ]]; then
        printf "${C_YELLOW}--- Last 20 lines of server.log ---${C_RESET}\n" >&2
        tail -20 "${TMP_DIR}/server.log" >&2
    fi
    exit 1
}

# Temporary directories
TMP_DIR=$(mktemp -d "/tmp/onclaw-smoke.XXXXXX")
DATA_DIR="${TMP_DIR}/data"
mkdir -p "${DATA_DIR}"
# Root for on-disk agent workspace prompt documents. The server started below
# is pointed at this root; when the script attaches to an already-running
# server, a caller-set ONCLAW_DIR is honored instead.
ONCLAW_BASE_DIR="${ONCLAW_DIR:-${TMP_DIR}/.onclaw}"
WS_ROOT="${ONCLAW_BASE_DIR}/workspaces"

SERVER_PID=""
MOCK_PID=""
WA_STUB_PID=""

cleanup() {
    local exit_code=$?
    if [[ -n "${SERVER_PID}" ]]; then
        log_info "Stopping background server (PID: ${SERVER_PID})..."
        kill "${SERVER_PID}" 2>/dev/null || true
        wait "${SERVER_PID}" 2>/dev/null || true
    fi
    if [[ -n "${MOCK_PID}" ]]; then
        kill "${MOCK_PID}" 2>/dev/null || true
        wait "${MOCK_PID}" 2>/dev/null || true
    fi
    if [[ -n "${WA_STUB_PID}" ]]; then
        kill "${WA_STUB_PID}" 2>/dev/null || true
        wait "${WA_STUB_PID}" 2>/dev/null || true
    fi
    if [[ -n "${HB_MOCK_PID}" ]]; then
        kill "${HB_MOCK_PID}" 2>/dev/null || true
        wait "${HB_MOCK_PID}" 2>/dev/null || true
    fi
    rm -rf "${TMP_DIR}"
    if [[ $exit_code -eq 0 ]]; then
        printf "\n${C_BOLD}${C_GREEN}🎉 All smoke tests passed successfully!${C_RESET}\n\n"
    else
        printf "\n${C_BOLD}${C_RED}💥 Smoke test suite failed!${C_RESET}\n\n"
    fi
}
trap cleanup EXIT INT TERM

# Helper: make HTTP request
# Sets $HTTP_STATUS and $HTTP_BODY
HTTP_STATUS=""
HTTP_BODY=""

api_req() {
    local method="$1"
    local path="$2"
    local token="${3:-}"
    local body="${4:-}"
    local content_type="${5:-application/json}"
    local extra_header="${6:-}"   # optional "Name: value" header (section 24's X-Forwarded-Proto)

    local url="${SERVER_URL}${path}"
    local headers_file="${TMP_DIR}/headers.tmp"
    local body_file="${TMP_DIR}/body.tmp"

    local args=(-s -S -X "${method}" -D "${headers_file}" -o "${body_file}")

    if [[ -n "${token}" ]]; then
        args+=(-H "Authorization: Bearer ${token}")
    fi

    if [[ -n "${body}" ]]; then
        args+=(-H "Content-Type: ${content_type}" --data "${body}")
    fi

    if [[ -n "${extra_header}" ]]; then
        args+=(-H "${extra_header}")
    fi

    if ! curl "${args[@]}" "${url}"; then
        log_fail "curl failed to reach ${url}"
    fi

    # Extract the FINAL HTTP status code — large bodies get an interim
    # "HTTP/1.1 100 Continue" header block from curl's Expect handshake, so
    # the first header line is not necessarily the response status.
    HTTP_STATUS=$(awk '/^HTTP\/[0-9.]+/ {code=$2} END {print code}' "${headers_file}")
    HTTP_BODY=$(cat "${body_file}")
    if [[ "${HTTP_STATUS}" == "400" && "${HTTP_BODY}" == *"Bad Request"* ]]; then
        printf "${C_YELLOW}DEBUG: curl args: %s | URL: %s | headers: %s${C_RESET}\n" "${args[*]}" "${url}" "$(cat "${headers_file}")" >&2
    fi
}

api_upload() {
    local path="$1"
    local token="$2"
    local file_path="$3"
    local field_name="${4:-avatar}"

    local url="${SERVER_URL}${path}"
    local headers_file="${TMP_DIR}/headers.tmp"
    local body_file="${TMP_DIR}/body.tmp"

    local args=(-s -S -X POST -D "${headers_file}" -o "${body_file}")
    args+=(-H "Authorization: Bearer ${token}")
    args+=(-F "${field_name}=@${file_path}")

    curl "${args[@]}" "${url}" || log_fail "curl upload failed to reach ${url}"

    # Final status, not the interim 100 Continue block (see api_req).
    HTTP_STATUS=$(awk '/^HTTP\/[0-9.]+/ {code=$2} END {print code}' "${headers_file}")
    HTTP_BODY=$(cat "${body_file}")
}

assert_status() {
    local expected="$1"
    local msg="$2"
    if [[ "${HTTP_STATUS}" == "${expected}" ]]; then
        log_pass "${msg} (status: ${HTTP_STATUS})"
    else
        log_fail "${msg}: expected HTTP ${expected}, got ${HTTP_STATUS}. Body: ${HTTP_BODY}"
    fi
}

assert_json_expr() {
    local expr="$1"
    local msg="$2"
    local val
    val=$(echo "${HTTP_BODY}" | jq -r "${expr}" 2>/dev/null || echo "JQ_ERROR")
    if [[ "${val}" == "true" ]]; then
        log_pass "${msg}"
    else
        log_fail "${msg}: expression '${expr}' evaluated to '${val}'. Body: ${HTTP_BODY}"
    fi
}

json_get() {
    local expr="$1"
    echo "${HTTP_BODY}" | jq -r "${expr}"
}

# -----------------------------------------------------------------------------
# Check / Start Server
# -----------------------------------------------------------------------------
log_step "Initializing Environment"

# Verify required tools
for tool in curl jq go; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        log_fail "Required tool '$tool' is not installed or not in PATH."
    fi
done

# Check if server is already responding
IS_RUNNING=0
if curl -s -f -o /dev/null "${SERVER_URL}/healthz" 2>/dev/null; then
    IS_RUNNING=1
    log_info "Server is already running at ${SERVER_URL}."
else
    log_info "Starting OnClaw server on ${SERVER_URL} against ${DATABASE_URL}..."

    # Apply database migrations first
    log_info "Applying migrations..."
    go run . migrate up --database-url "${DATABASE_URL}" >/dev/null || {
        log_fail "Failed to apply database migrations against ${DATABASE_URL}. Ensure PostgreSQL is accessible."
    }

    # Build and run the server binary directly: `go run` would spawn a child
    # binary that outlives the killed wrapper PID and orphan the server.
    go build -o "${TMP_DIR}/onclaw-smoke-bin" . || log_fail "Failed to build the server binary"
    DATABASE_URL="${DATABASE_URL}" \
    ONCLAW_ENCRYPTION_KEY="${ENCRYPTION_KEY}" \
    ONCLAW_SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL}" \
    ONCLAW_SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD}" \
    ONCLAW_JWT_SECRET="${JWT_SECRET}" \
    ONCLAW_DATA_DIR="${DATA_DIR}" \
    ONCLAW_DIR="${ONCLAW_BASE_DIR}" \
    ONCLAW_LISTEN_ADDR="${SERVER_HOST}:${SERVER_PORT}" \
    ONCLAW_WHATSAPP_CLOUD_API_BASE="${WA_STUB_BASE}" \
    "${TMP_DIR}/onclaw-smoke-bin" server --database-url "${DATABASE_URL}" --listen-addr "${SERVER_HOST}:${SERVER_PORT}" --encryption-key "${ENCRYPTION_KEY}" >"${TMP_DIR}/server.log" 2>&1 &

    SERVER_PID=$!
    log_info "Server started with PID ${SERVER_PID}. Waiting for healthz..."

    READY=0
    for i in {1..30}; do
        if curl -s -f "${SERVER_URL}/healthz" >/dev/null 2>&1; then
            READY=1
            break
        fi
        sleep 0.3
    done

    if [[ $READY -ne 1 ]]; then
        log_fail "Server failed to start within timeout. Logs:\n$(cat "${TMP_DIR}/server.log")"
    fi
    log_pass "Server is healthy and ready."
fi

# -----------------------------------------------------------------------------
# 1. Health Endpoints
# -----------------------------------------------------------------------------
log_step "1. Health Checks"

api_req "GET" "/healthz"
assert_status "200" "GET /healthz"
assert_json_expr '.status == "ok"' "Healthz returns status ok"

api_req "GET" "/api/v1/health"
assert_status "200" "GET /api/v1/health"
assert_json_expr '.status == "ok"' "API v1 health returns status ok"

# -----------------------------------------------------------------------------
# 2. Superadmin Authentication & Failure Modes
# -----------------------------------------------------------------------------
log_step "2. Authentication Matrix"

# Test 2.1: Login with invalid password -> 401
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"WrongPassword!\"}"
assert_status "401" "Login with wrong password returns 401"
assert_json_expr '.error.code == "unauthenticated"' "Error code is unauthenticated"

# Test 2.2: Login with unsupported provider -> 400
api_req "POST" "/api/v1/auth/login" "" "{\"provider\":\"unsupported_saml\",\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"secret\"}"
assert_status "400" "Login with unsupported provider returns 400"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# Test 2.3: Valid superadmin login -> 200
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"${SUPERADMIN_PASSWORD}\"}"
assert_status "200" "Superadmin login with valid credentials returns 200"
SUPERADMIN_TOKEN=$(json_get '.token')
SUPERADMIN_UID=$(json_get '.user.id')

if [[ -z "${SUPERADMIN_TOKEN}" || "${SUPERADMIN_TOKEN}" == "null" ]]; then
    log_fail "Failed to retrieve JWT token from superadmin login response"
fi
log_pass "Retrieved superadmin JWT token: ${SUPERADMIN_TOKEN:0:15}..."

# -----------------------------------------------------------------------------
# 3. Auth Me
# -----------------------------------------------------------------------------
log_step "3. Auth /me Verification"

# Test 3.1: Unauthenticated /auth/me -> 401
api_req "GET" "/api/v1/auth/me" ""
assert_status "401" "GET /auth/me without token returns 401"

# Test 3.2: Authenticated /auth/me -> 200
api_req "GET" "/api/v1/auth/me" "${SUPERADMIN_TOKEN}"
assert_status "200" "GET /auth/me with superadmin token returns 200"
assert_json_expr ".user.email == \"${SUPERADMIN_EMAIL}\"" "User email matches superadmin email"
assert_json_expr '(.memberships | length) > 0' "Superadmin has workspace memberships"
assert_json_expr '.memberships[] | select(.workspace_slug == "master") | .role_name == "Superadmin"' "Superadmin holds Superadmin role in master workspace"

# -----------------------------------------------------------------------------
# 4. Master Protection & Admin Workspace Management
# -----------------------------------------------------------------------------
log_step "4. Master Protection & Admin Workspace Creation"

# Test 4.1: Cannot disable master workspace -> 400
api_req "POST" "/api/v1/admin/workspaces/master/disable" "${SUPERADMIN_TOKEN}"
assert_status "400" "Admin cannot disable master workspace (400)"

# Test 4.2: Cannot create workspace with reserved slug 'master' -> 400
api_req "POST" "/api/v1/admin/workspaces" "${SUPERADMIN_TOKEN}" '{"name":"Master Duplicate","slug":"master","owner_email":"someone@onclaw.local"}'
assert_status "400" "Cannot create workspace with reserved slug 'master' (400)"

# Test 4.3: Pre-create owner user (Alice) with password
RUN_ID="$(date +%s)-${RANDOM}"
ALICE_EMAIL="alice.${RUN_ID}@example.com"
ALICE_PASSWORD="AliceSmokePassword123!"
api_req "POST" "/api/v1/admin/users" "${SUPERADMIN_TOKEN}" "{\"email\":\"${ALICE_EMAIL}\",\"name\":\"Alice Smoke\",\"password\":\"${ALICE_PASSWORD}\"}"
assert_status "201" "Admin pre-creates user Alice with credentials"
ALICE_UID=$(json_get '.user.id')

# Test 4.4: Admin creates new tenant 'smoke-tenant' assigning Alice as owner
TENANT_SLUG="smoke-tenant-${RUN_ID}"
api_req "POST" "/api/v1/admin/workspaces" "${SUPERADMIN_TOKEN}" "{\"name\":\"Smoke Tenant\",\"slug\":\"${TENANT_SLUG}\",\"owner_email\":\"${ALICE_EMAIL}\"}"
assert_status "201" "Admin creates tenant '${TENANT_SLUG}' with owner Alice"
TENANT_ID=$(json_get '.workspace.id')

# Test 4.5: Unscoped admin workspace listing
api_req "GET" "/api/v1/admin/workspaces" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin list workspaces unscoped returns 200"
assert_json_expr ".workspaces[] | select(.slug == \"${TENANT_SLUG}\") | .member_count == 1" "Created tenant listed with member count 1"

# -----------------------------------------------------------------------------
# 5. Workspace Suspend / Restore Lifecycle
# -----------------------------------------------------------------------------
log_step "5. Suspend and Restore Lifecycle"

# Login as Alice
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${ALICE_EMAIL}\",\"password\":\"${ALICE_PASSWORD}\"}"
assert_status "200" "Alice logs in successfully"
ALICE_TOKEN=$(json_get '.token')

# Alice accesses her workspace before suspension -> 200
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${ALICE_TOKEN}"
assert_status "200" "Alice accesses workspace before suspension (200)"

# Admin suspends tenant
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/disable" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin suspends tenant (200)"

# Alice accesses suspended workspace -> 403 Forbidden
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${ALICE_TOKEN}"
assert_status "403" "Member accessing suspended workspace receives 403 Forbidden"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

# Admin restores tenant
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/enable" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin restores tenant (200)"

# Alice accesses restored workspace -> 200
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${ALICE_TOKEN}"
assert_status "200" "Alice accesses restored workspace (200)"

# -----------------------------------------------------------------------------
# 6. Workspace Member Management & Permission Algebra Guards
# -----------------------------------------------------------------------------
log_step "6. Member Management & Permission Algebra Guards"

# List roles in workspace
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/roles" "${ALICE_TOKEN}"
assert_status "200" "Alice lists roles in workspace"
OWNER_ROLE_ID=$(json_get '.roles[] | select(.name == "Owner") | .id')
ADMIN_ROLE_ID=$(json_get '.roles[] | select(.name == "Admin") | .id')
MEMBER_ROLE_ID=$(json_get '.roles[] | select(.name == "Member") | .id')

log_info "Workspace Roles -> Owner: ${OWNER_ROLE_ID}, Admin: ${ADMIN_ROLE_ID}, Member: ${MEMBER_ROLE_ID}"

# Create Bob and Charlie via Admin API
BOB_EMAIL="bob.${RUN_ID}@example.com"
BOB_PASSWORD="BobSmokePassword123!"
api_req "POST" "/api/v1/admin/users" "${SUPERADMIN_TOKEN}" "{\"email\":\"${BOB_EMAIL}\",\"name\":\"Bob Smoke\",\"password\":\"${BOB_PASSWORD}\"}"
assert_status "201" "Admin creates user Bob"
BOB_UID=$(json_get '.user.id')

CHARLIE_EMAIL="charlie.${RUN_ID}@example.com"
CHARLIE_PASSWORD="CharlieSmokePassword123!"
api_req "POST" "/api/v1/admin/users" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"name\":\"Charlie Smoke\",\"password\":\"${CHARLIE_PASSWORD}\"}"
assert_status "201" "Admin creates user Charlie"
CHARLIE_UID=$(json_get '.user.id')

# Alice adds Bob as Admin
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${ALICE_TOKEN}" "{\"email\":\"${BOB_EMAIL}\",\"role_id\":\"${ADMIN_ROLE_ID}\"}"
assert_status "201" "Alice adds Bob as Admin"

# Alice adds Charlie as Member
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${ALICE_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Alice adds Charlie as Member"

# Verify 3 members
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/members" "${ALICE_TOKEN}"
assert_status "200" "Alice lists members in workspace"
assert_json_expr '(.members | length) == 3' "Workspace now has 3 members"

# Bob logs in
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${BOB_EMAIL}\",\"password\":\"${BOB_PASSWORD}\"}"
assert_status "200" "Bob logs in"
BOB_TOKEN=$(json_get '.token')

# Guard: Bob (Admin) cannot assign Owner role (canAssign: role !⊆ actor) -> 403
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${BOB_TOKEN}" "{\"email\":\"dave.smoke@example.com\",\"role_id\":\"${OWNER_ROLE_ID}\"}"
assert_status "403" "Admin cannot assign Owner role (canAssign guard returns 403)"

# Alice updates Charlie to Admin role
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${CHARLIE_UID}" "${ALICE_TOKEN}" "{\"role_id\":\"${ADMIN_ROLE_ID}\"}"
assert_status "200" "Alice updates Charlie's role to Admin"

# Guard: Bob (Admin) cannot edit/demote Charlie (Admin) (canEdit: target !⊊ actor) -> 403
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${CHARLIE_UID}" "${BOB_TOKEN}" "{\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "403" "Admin cannot modify peer Admin member (canEdit guard returns 403)"

# Alice removes Charlie from workspace
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/members/${CHARLIE_UID}" "${ALICE_TOKEN}"
assert_status "204" "Alice removes Charlie from workspace (204)"

# -----------------------------------------------------------------------------
# 7. Multi-Tenant Enumeration Defense (404-over-403)
# -----------------------------------------------------------------------------
log_step "7. Multi-Tenant Enumeration Defense & 404-over-403"

# Charlie logs in
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${CHARLIE_EMAIL}\",\"password\":\"${CHARLIE_PASSWORD}\"}"
assert_status "200" "Charlie logs in"
CHARLIE_TOKEN=$(json_get '.token')

# Charlie (non-member) accessing tenant -> 404 Not Found (enumeration defense)
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}"
assert_status "404" "Non-member receives 404 Not Found (enumeration defense)"

# Superadmin accessing non-master tenant without membership -> 404 Not Found (no bypass)
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${SUPERADMIN_TOKEN}"
assert_status "404" "Superadmin without tenant membership receives 404 Not Found (no bypass)"

# -----------------------------------------------------------------------------
# 8. Avatar Upload, Magic Byte Sniffing, & Capability URL Serving
# -----------------------------------------------------------------------------
log_step "8. Avatar Upload & Capability Serving"

# Create a valid 1x1 PNG image in temp file
PNG_FILE="${TMP_DIR}/test_avatar.png"
echo "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==" | base64 -d > "${PNG_FILE}"

# Create an invalid text file for magic-byte rejection test
TEXT_FILE="${TMP_DIR}/fake_image.png"
echo "not a real png file content" > "${TEXT_FILE}"

# Test 8.1: Upload invalid avatar -> 400 Bad Request
api_upload "/api/v1/users/me/avatar" "${ALICE_TOKEN}" "${TEXT_FILE}" "avatar"
assert_status "400" "Invalid avatar content rejected (magic byte sniff 400)"

# Test 8.2: Upload valid PNG avatar -> 200 OK
api_upload "/api/v1/users/me/avatar" "${ALICE_TOKEN}" "${PNG_FILE}" "avatar"
assert_status "200" "Valid PNG avatar uploaded successfully (200)"
AVATAR_URL=$(json_get '.avatar_url')
log_info "Received avatar URL: ${AVATAR_URL}"

# Test 8.3: Public capability file serving (unauthenticated curl)
api_req "GET" "${AVATAR_URL}" ""
assert_status "200" "Capability avatar URL fetched without authentication (200)"

# Test 8.4: Profile PATCH name
api_req "PATCH" "/api/v1/users/me" "${ALICE_TOKEN}" '{"name":"Alice In Wonderland"}'
assert_status "200" "Alice updates self profile name (200)"
assert_json_expr '.user.name == "Alice In Wonderland"' "User profile reflects updated name"

# -----------------------------------------------------------------------------
# 9. Last-Owner & Last-Admin Protection Guards
# -----------------------------------------------------------------------------
log_step "9. Last-Owner & Last-Admin Protection Guards"

# Test 9.1: Alice (sole owner) attempts to demote herself to Member -> rejected (409 last-owner error)
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${ALICE_UID}" "${ALICE_TOKEN}" "{\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "409" "Demoting the sole owner is rejected (last-owner guard 409)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# Test 9.2: Alice (sole owner) attempts to delete herself -> rejected (409)
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/members/${ALICE_UID}" "${ALICE_TOKEN}"
assert_status "409" "Deleting the sole owner is rejected (last-owner guard 409)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# Test 9.3: Revoking the only Superadmin in master tenant -> rejected (409)
api_req "DELETE" "/api/v1/admin/superadmins/${SUPERADMIN_UID}" "${SUPERADMIN_TOKEN}"
assert_status "409" "Revoking the only superadmin in master tenant is rejected (last-admin guard 409)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# Test 9.4: Grant second superadmin and then revoke
api_req "POST" "/api/v1/admin/superadmins" "${SUPERADMIN_TOKEN}" "{\"user_id\":\"${BOB_UID}\"}"
assert_status "201" "Superadmin grants Bob superadmin role in master tenant (201)"

api_req "DELETE" "/api/v1/admin/superadmins/${BOB_UID}" "${SUPERADMIN_TOKEN}"
assert_status "204" "Superadmin revokes Bob's superadmin role when another superadmin exists (204)"

# Test 9.5: Global user disable / enable
api_req "POST" "/api/v1/admin/users/${BOB_UID}/disable" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin globally disables user Bob (200)"

# Disabled user cannot login -> 401
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${BOB_EMAIL}\",\"password\":\"${BOB_PASSWORD}\"}"
assert_status "401" "Disabled user Bob cannot log in (401)"

# Admin re-enables user
api_req "POST" "/api/v1/admin/users/${BOB_UID}/enable" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin globally re-enables user Bob (200)"

# Re-enabled user can login -> 200
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${BOB_EMAIL}\",\"password\":\"${BOB_PASSWORD}\"}"
assert_status "200" "Re-enabled user Bob logs in successfully (200)"

# -----------------------------------------------------------------------------
# 10. CLI User Provisioning & Authentication Verification
# -----------------------------------------------------------------------------
log_step "10. CLI User Provisioning & Authentication Verification"

CLI_USER_EMAIL="cli.user.${RUN_ID}@example.com"
CLI_USER_NAME="CLI User ${RUN_ID}"
CLI_USER_PASSWORD="CliUserPassword123!"

log_info "Creating user via CLI: ${CLI_USER_EMAIL}..."
go run . user create \
    --database-url "${DATABASE_URL}" \
    --email "${CLI_USER_EMAIL}" \
    --name "${CLI_USER_NAME}" \
    --password "${CLI_USER_PASSWORD}" >/dev/null || {
    log_fail "Failed to create user via CLI"
}
log_pass "User created successfully via CLI command"

# Verify created user can authenticate via API
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${CLI_USER_EMAIL}\",\"password\":\"${CLI_USER_PASSWORD}\"}"
assert_status "200" "CLI-created user logs in successfully via API (200)"
assert_json_expr ".user.email == \"${CLI_USER_EMAIL}\"" "User email matches CLI input"
assert_json_expr ".user.name == \"${CLI_USER_NAME}\"" "User name matches CLI input"

CLI_USER_TOKEN=$(json_get '.token')
if [[ -z "${CLI_USER_TOKEN}" || "${CLI_USER_TOKEN}" == "null" ]]; then
    log_fail "Failed to retrieve JWT token for CLI-created user"
fi

# Verify /me endpoint with CLI user token
api_req "GET" "/api/v1/auth/me" "${CLI_USER_TOKEN}"
assert_status "200" "CLI user retrieves /auth/me profile (200)"
assert_json_expr ".user.email == \"${CLI_USER_EMAIL}\"" "/auth/me returns correct user"

# -----------------------------------------------------------------------------
# 11. Admin Tenant Control
# -----------------------------------------------------------------------------
log_step "11. Admin Tenant Control"

# 11.1 Patch tenant details
api_req "PATCH" "/api/v1/admin/workspaces/${TENANT_SLUG}" "${SUPERADMIN_TOKEN}" '{"name":"Renamed Smoke Tenant", "timezone":"Europe/London"}'
assert_status "200" "Admin patches workspace name and timezone"
assert_json_expr ".workspace.name == \"Renamed Smoke Tenant\"" "Workspace name was updated"
assert_json_expr ".workspace.timezone == \"Europe/London\"" "Workspace timezone was updated"

# 11.2 Admin lists tenant members
api_req "GET" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}"
assert_status "200" "Admin lists tenant members"
assert_json_expr "(.members | length) >= 2" "Admin sees tenant members"

# 11.3 Admin adds member to tenant (Charlie as Member)
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Admin adds Charlie to tenant as Member"

# Guard: Admin attempts to add user with Owner role -> 400
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CLI_USER_EMAIL}\",\"role_id\":\"${OWNER_ROLE_ID}\"}"
assert_status "400" "Admin adding member with Owner role is rejected"

# Guard: Admin attempts to add unknown user -> 404
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"nobody@nowhere.test\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "404" "Admin adding unknown user is rejected"

# Guard: Admin attempts to add already-member -> 409
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "409" "Admin adding already-member is rejected"

# 11.4 Admin transfers ownership (Alice -> Charlie)
api_req "PATCH" "/api/v1/admin/workspaces/${TENANT_SLUG}/owner" "${SUPERADMIN_TOKEN}" "{\"user_id\":\"${CHARLIE_UID}\"}"
assert_status "200" "Admin transfers ownership to Charlie"

# Add CLI user to tenant as Member for provider permission tests
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CLI_USER_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Admin adds CLI user to tenant as Member"

# -----------------------------------------------------------------------------
# 12. Workspace Provider Configuration CRUD & Verify Scenarios
# -----------------------------------------------------------------------------
log_step "12. Workspace Provider Configuration CRUD & Verify"

# 12.1 CLI User (Member) lists providers -> 200 OK (empty list)
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CLI_USER_TOKEN}"
assert_status "200" "Member lists provider configurations"
assert_json_expr "(.providers | length) == 0" "Providers list is initially empty"

# 12.2 Member attempts to create a provider -> 403 Forbidden
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CLI_USER_TOKEN}" '{"type":"openai","name":"Unauthorized Provider"}'
assert_status "403" "Member cannot create provider (403 Forbidden)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

# 12.3 Charlie (Owner) creates an OpenAI provider with key
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai","name":"OpenAI Prod","key":"sk-smoke-secret-key-12345678"}'
assert_status "201" "Owner creates OpenAI provider with key"
assert_json_expr '.provider.type == "openai"' "Provider type is openai"
assert_json_expr '.provider.name == "OpenAI Prod"' "Provider name is OpenAI Prod"
assert_json_expr '.provider.key_set == true' "Provider has key_set: true"
assert_json_expr '.provider.key_hint == "5678"' "Provider key_hint is 5678"
assert_json_expr '.provider.enabled == true' "Provider defaults to enabled: true"
OPENAI_PROV_ID=$(json_get '.provider.id')

# Verify key secrecy: no plaintext key or ciphertext in response
if echo "${HTTP_BODY}" | grep -q "sk-smoke-secret-key-12345678"; then
    log_fail "Plaintext API key leaked in create provider response!"
fi
if echo "${HTTP_BODY}" | grep -q "v1:"; then
    log_fail "Ciphertext envelope leaked in create provider response!"
fi
log_pass "Key secrecy verified: no key material in response body"

# 12.4 Validation: Compatible type requires base_url -> 400
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Local vLLM"}'
assert_status "400" "Compatible provider without base_url is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# 12.4a Validation: Unknown provider type -> 400
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"azure-openai","name":"Azure"}'
assert_status "400" "Unknown provider type is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# 12.4b Validation: Invalid URL scheme -> 400
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"FTP API","base_url":"ftp://127.0.0.1:8000/v1"}'
assert_status "400" "Provider base_url with non-http(s) scheme is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# 12.5 Owner creates compatible provider with valid base_url
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Local vLLM","base_url":"http://127.0.0.1:8000/v1"}'
assert_status "201" "Owner creates openai-compatible provider with base_url"
assert_json_expr '.provider.base_url == "http://127.0.0.1:8000/v1"' "Provider base_url is set"
COMPAT_PROV_ID=$(json_get '.provider.id')

# 12.6 Verify keyless provider -> 400 invalid_request
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers/${COMPAT_PROV_ID}/verify" "${CHARLIE_TOKEN}"
assert_status "400" "Verifying keyless provider returns 400 invalid_request"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# 12.7 Verify with key: returns 200 {ok: bool, error?: string}
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers/${OPENAI_PROV_ID}/verify" "${CHARLIE_TOKEN}"
assert_status "200" "Verifying configured provider returns 200 OK"
assert_json_expr 'has("ok")' "Verify response contains ok field"

# 12.8 Member attempts to verify provider -> 403 Forbidden
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers/${OPENAI_PROV_ID}/verify" "${CLI_USER_TOKEN}"
assert_status "403" "Member cannot verify provider (403 Forbidden)"

# 12.9 Patch provider: update name, toggle enabled, key omitted (preserves key)
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/providers/${OPENAI_PROV_ID}" "${CHARLIE_TOKEN}" '{"name":"Renamed OpenAI Prod","enabled":false}'
assert_status "200" "Owner patches provider name and enabled"
assert_json_expr '.provider.name == "Renamed OpenAI Prod"' "Provider name updated"
assert_json_expr '.provider.enabled == false' "Provider enabled toggled to false"
assert_json_expr '.provider.key_set == true' "Key remains set"
assert_json_expr '.provider.key_hint == "5678"' "Key hint preserved"

# 12.10 Patch provider with empty key -> 400 invalid_request
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/providers/${OPENAI_PROV_ID}" "${CHARLIE_TOKEN}" '{"key":""}'
assert_status "400" "Patching provider with empty key is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Error code is invalid_request"

# 12.11 Delete provider -> 204 No Content
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/providers/${COMPAT_PROV_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes provider (204 No Content)"

# 12.12 Subsequent GET/PATCH of deleted provider -> 404
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/providers/${COMPAT_PROV_ID}" "${CHARLIE_TOKEN}" '{"name":"Should Fail"}'
assert_status "404" "Accessing deleted provider returns 404 Not Found"



# -----------------------------------------------------------------------------

log_step "Smoke Test Suite Completed Successfully"

# 13. Agents & Skills CRUD, Memory, Models, and Birth Flow
# -----------------------------------------------------------------------------
log_step "13. Agents, Skills, Models, and Workspace Birth Flow"

# 13.0 Mock OpenAI-compatible provider server. Create-time prompt generation
# gates agent persistence, so the create assertions need a provider whose
# /v1/chat/completions actually succeeds; a python one-liner plays the part.
MOCK_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
cat > "${TMP_DIR}/mock_provider.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1])

ARGS = json.dumps({
    "identity": "# IDENTITY.md - Who Am I?\n**Name:** Smoke Agent\n**Creature:** test fixture\n**Purpose:** smoke the create path\n**Vibe:** deterministic\n**Emoji:** checkmark\n",
    "soul": "# SOUL.md\nShort beats long. Deterministic beats flaky.\nBe the assistant you'd actually want to talk to at 2am. Not a corporate drone. Not a sycophant. Just... good.\n",
    "bootstrap": "# BOOTSTRAP.md - Birth Sequence\n_You just woke up. Keep this first conversation short and make it yours._\n",
})

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            self._send(200, {"data": [{"id": "gpt-4"}, {"id": "gpt-4o"}]})
        else:
            self._send(401, {"error": {"message": "Invalid API key"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(raw)
        except ValueError:
            body = {}
        # Live-chat turns (section 14) are marked in the user input; they get
        # a plain assistant reply. Everything else is the create/regenerate
        # prompt-generation flow and receives the submit_prompts tool call.
        messages = body.get("messages", [])

        def content_text(m):
            # The agent runner sends content as a list of typed blocks
            # ([{"type": "text", "text": ...}]), while other callers send a
            # plain string; the marker can live in either shape.
            c = m.get("content")
            if isinstance(c, str):
                return c
            if isinstance(c, list):
                return " ".join(p.get("text", "") for p in c if isinstance(p, dict))
            return ""

        all_text = " ".join(content_text(m) for m in messages if isinstance(m, dict))

        # Section 18 teams branch (channel-teams): the kickoff body carries
        # ONCLAW_TEAMS_SMOKE and every in-session auto-post carries the same
        # marker, so every run of the materialized team lands here. The chain
        # advances by replying with an @mention of the next specialist; the
        # deepest hop tags the human (@charlie-smoke), which pauses the
        # session. ONCLAW_TEAMS_CLOSE steers the facilitator's run to the
        # session.close tool call instead of a plain reply.
        is_teams = "ONCLAW_TEAMS_SMOKE" in all_text
        is_close = "ONCLAW_TEAMS_CLOSE" in all_text

        def teams_reply(text):
            # Stage detection rides explicit markers, NOT bare @handles: the
            # composed input carries the roster, so every handle appears in
            # every run's context. The stage markers exist only in the chain's
            # own messages and accumulate, so the highest marker present
            # identifies the current run: S2 = the pm was summoned, S3 = the
            # architect, S4 = the backend, S5 = the tester (who then tags the
            # human, pausing the session as awaiting-human).
            if "ONCLAW_TEAMS_S5" in text:
                return "@charlie-smoke ONCLAW_TEAMS_SMOKE build and test are complete — requesting human sign-off to proceed."
            if "ONCLAW_TEAMS_S4" in text:
                return "@tester ONCLAW_TEAMS_SMOKE ONCLAW_TEAMS_S5 the build is done, please verify against the acceptance criteria."
            if "ONCLAW_TEAMS_S3" in text:
                return "@backend ONCLAW_TEAMS_SMOKE ONCLAW_TEAMS_S4 the spec is in /project/spec.md, please implement the service side."
            if "ONCLAW_TEAMS_S2" in text:
                return "@architect ONCLAW_TEAMS_SMOKE ONCLAW_TEAMS_S3 please draft the technical spec and cut the scope."
            return "@pm ONCLAW_TEAMS_SMOKE ONCLAW_TEAMS_S2 kickoff received — I have sequenced the work, starting with the plan."

        if is_live := ("ONCLAW_V1_SMOKE" in all_text):
            # The runner always executes turns in streaming mode; a compliant
            # server must answer stream:true with an SSE chat.completion.chunk
            # sequence, otherwise no assistant message is ever assembled (and
            # none persists into the session history).
            if body.get("stream"):
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()

                def sse(chunk):
                    self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())

                def chunk(delta, finish):
                    return {
                        "id": "chatcmpl-smoke-live",
                        "object": "chat.completion.chunk",
                        "created": 0,
                        "model": "gpt-4",
                        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
                    }

                sse(chunk({"role": "assistant"}, None))
                for piece in ["live reply", " from the", " smoke mock"]:
                    sse(chunk({"content": piece}, None))
                final = chunk({}, "stop")
                # OpenAI include_usage convention: the terminal chunk carries
                # the usage block so streamed turns report token usage (the
                # compact-command section asserts completed-with-usage).
                final["usage"] = {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11}
                sse(final)
                self.wfile.write(b"data: [DONE]\n\n")
                return
            payload = {
                "id": "chatcmpl-smoke-live",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "live reply from the smoke mock"},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11},
            }
            self._send(200, payload)
            return
        if is_teams or is_close:
            if body.get("stream") and is_close and "@scrum-master" in all_text:
                # The facilitator close leg: steer the run to the session.close
                # tool via a streaming tool_calls frame pair, then finish with
                # finish_reason "tool_calls" so the runner executes the tool.
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()

                def sse_close(part):
                    self.wfile.write(("data: " + json.dumps(part) + "\n\n").encode())

                def close_chunk(delta, finish):
                    return {
                        "id": "chatcmpl-smoke-close",
                        "object": "chat.completion.chunk",
                        "created": 0,
                        "model": "gpt-4",
                        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
                    }

                sse_close(close_chunk({"role": "assistant"}, None))
                sse_close(close_chunk({"tool_calls": [{"index": 0, "id": "call_close", "type": "function", "function": {"name": "session.close", "arguments": "{\"summary\":"}}]}, None))
                sse_close(close_chunk({"tool_calls": [{"index": 0, "function": {"arguments": "\"Dark mode shipped and verified; follow-ups tracked in PLAN.md.\"}"}}]}, None))
                sse_close(close_chunk({}, "tool_calls"))
                self.wfile.write(b"data: [DONE]\n\n")
                return
            reply = teams_reply(all_text)
            if body.get("stream"):
                self.send_response(200)
                self.send_header("Content-Type", "text/event-stream")
                self.end_headers()

                def sse_teams(part):
                    self.wfile.write(("data: " + json.dumps(part) + "\n\n").encode())

                def teams_chunk(delta, finish):
                    return {
                        "id": "chatcmpl-smoke-teams",
                        "object": "chat.completion.chunk",
                        "created": 0,
                        "model": "gpt-4",
                        "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
                    }

                sse_teams(teams_chunk({"role": "assistant"}, None))
                for piece in [reply]:
                    sse_teams(teams_chunk({"content": piece}, None))
                sse_teams(teams_chunk({}, "stop"))
                self.wfile.write(b"data: [DONE]\n\n")
                return
            payload = {
                "id": "chatcmpl-smoke-teams",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": reply},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11},
            }
            self._send(200, payload)
            return
        if self.path.startswith("/v1/chat/completions"):
            payload = {
                "id": "chatcmpl-smoke",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "", "tool_calls": [{
                        "id": "call_smoke",
                        "type": "function",
                        "function": {"name": "submit_prompts", "arguments": ARGS},
                    }]},
                    "finish_reason": "tool_calls",
                }],
            }
            self._send(200, payload)
        else:
            self._send(401, {"error": {"message": "Invalid API key"}})

HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
python3 "${TMP_DIR}/mock_provider.py" "${MOCK_PORT}" &
MOCK_PID=$!
sleep 0.5
log_pass "Mock provider server listening on 127.0.0.1:${MOCK_PORT}"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Smoke Mock Provider","base_url":"http://127.0.0.1:'"${MOCK_PORT}"'/v1","key":"sk-mock-key"}'
assert_status "201" "Owner creates openai-compatible provider against the mock server"
MOCK_PROV_ID=$(json_get '.provider.id')

# 13.1 Workspace skills: author install, tier-gated listing, enable/disable,
# uninstall, and the Member write guard. The routes are part of the live
# contract (workspace-skills-ui); no 404 skip anymore.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/skills" "${CHARLIE_TOKEN}" '{"source":"authored","name":"search-web","description":"Search the web for a topic and summarize","body":"# search_web\n\nSearch the web and summarize the findings with sources.\n"}'
assert_status "201" "Owner installs an authored workspace skill"
assert_json_expr '.skill.name == "search-web"' "Install response carries the skill name"
assert_json_expr '.skill.tier == "workspace"' "Installed skill is workspace tier"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/skills" "${CHARLIE_TOKEN}" '{"source":"authored","name":"search-web","description":"Duplicate","body":"dup"}'
assert_status "409" "Duplicate skill name returns 409"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/skills" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists skills"
assert_json_expr '(.skills | length) >= 1' "Skills list contains the created skill"
assert_json_expr '([.skills[] | select(.tier == "system")] | length) >= 1' "List includes the locked system tier"
assert_json_expr '[.skills[] | select(.tier == "system")][0].locked == true' "System tier entries are locked"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${CHARLIE_TOKEN}"
assert_status "200" "Owner gets skill by name"
assert_json_expr '.skill.body | contains("Search the web")' "Skill body includes the authored SKILL.md"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "200" "Owner disables the skill (master switch)"
assert_json_expr '.skill.enabled == false' "Skill reports disabled"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${CHARLIE_TOKEN}" '{"enabled":true}'
assert_status "200" "Owner re-enables the skill"
assert_json_expr '.skill.enabled == true' "Skill reports enabled"

# Member write guard: a plain Member can read the library but not mutate it.
DAVE_EMAIL="dave.skills.${RUN_ID}@example.com"
DAVE_PASSWORD="dave-Skills-Pass1"
api_req "POST" "/api/v1/admin/users" "${SUPERADMIN_TOKEN}" "{\"email\":\"${DAVE_EMAIL}\",\"name\":\"Dave Skills Smoke\",\"password\":\"${DAVE_PASSWORD}\"}"
assert_status "201" "Superadmin creates Dave (skills guard fixture)"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${ALICE_TOKEN}" "{\"email\":\"${DAVE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Alice adds Dave as Member"

api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${DAVE_EMAIL}\",\"password\":\"${DAVE_PASSWORD}\"}"
assert_status "200" "Dave logs in"
DAVE_TOKEN=$(json_get '.token')

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/skills" "${DAVE_TOKEN}"
assert_status "200" "Member can list skills (skills.read)"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/skills" "${DAVE_TOKEN}" '{"source":"authored","name":"nope","description":"x","body":"x"}'
assert_status "403" "Member cannot install skills (skills.write 403)"

api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${DAVE_TOKEN}"
assert_status "403" "Member cannot uninstall skills (403)"

api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${CHARLIE_TOKEN}"
assert_status "204" "Owner uninstalls the skill"
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/skills/search-web" "${CHARLIE_TOKEN}"
assert_status "404" "Uninstalled skill is gone"

# 13.2 Agents CRUD & Models Endpoint
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" '{"name":"Test Agent","slug":"test-agent","role":"Tester","description":"Tests things","brief":"A short brief","provider_id":"'"${MOCK_PROV_ID}"'","model":"gpt-4"}'
assert_status "201" "Owner creates an agent (generation gates persistence)"
assert_json_expr '.agent.prompts_status == "ready"' "Create returns the agent with prompts ready (generation ran against the mock provider)"
AGENT_ID=$(json_get '.agent.id')

# The agent workspace directory is created at create time; the L1 base prompt
# is injected at build time, never seeded to disk (markdown-card-elements D8
# sweeps seeded AGENTS.md out of agent workspaces). Generated documents
# (IDENTITY/SOUL/BOOTSTRAP.md) land in the same directory once generation
# succeeds against a live provider.
AGENT_WS_DIR="${WS_ROOT}/${TENANT_SLUG}/agents/test-agent"
if [[ -d "${AGENT_WS_DIR}" ]]; then
    log_pass "Agent workspace directory exists (${AGENT_WS_DIR})"
else
    log_fail "Agent workspace directory missing: ${AGENT_WS_DIR}"
fi
if [[ ! -f "${AGENT_WS_DIR}/AGENTS.md" ]]; then
    log_pass "No seeded AGENTS.md in agent workspace (base prompt injected at build)"
else
    log_fail "Agent workspace still carries a seeded AGENTS.md (should be swept)"
fi
GENERATED_STATUS=$(json_get '.agent.prompts_status')
if [[ "${GENERATED_STATUS}" == "ready" ]]; then
    for doc in IDENTITY.md SOUL.md BOOTSTRAP.md; do
        if [[ -f "${AGENT_WS_DIR}/${doc}" ]]; then
            log_pass "Generated prompt document present: ${doc}"
        else
            log_fail "Generated prompt document missing: ${AGENT_WS_DIR}/${doc}"
        fi
    done
else
    log_info "Prompt generation status '${GENERATED_STATUS}' — skipping generated-document assertions (needs a live provider)"
fi

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" '{"name":"Test Agent 2","slug":"test-agent","role":"Tester 2","description":"Duplicate slug","brief":"A short brief","provider_id":"'"${MOCK_PROV_ID}"'","model":"gpt-4"}'
assert_status "409" "Duplicate agent slug returns 409"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists agents"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent" "${CHARLIE_TOKEN}"
assert_status "200" "Owner gets agent by slug"
assert_json_expr '.agent.id == "'"${AGENT_ID}"'"' "Agent ID matches"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}" "${CHARLIE_TOKEN}" '{"name":"Updated Agent"}'
assert_status "200" "Owner updates agent"
assert_json_expr '.agent.name == "Updated Agent"' "Agent name updated"

# 13.2a Workspace default model (refactor-workspace-settings): PATCH set/replace,
# half-set 400, unknown provider 400, inherit agent create, clear-blocked 422
# with the inheriting count, then clear once the inheritor is gone.
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}" '{"default_model":{"provider_id":"'"${MOCK_PROV_ID}"'","model":"gpt-4o"}}'
assert_status "200" "Owner sets the workspace default model"
assert_json_expr '.workspace.default_model.model == "gpt-4o"' "Workspace payload carries the default model pair"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}"
assert_status "200" "Workspace read returns 200"
assert_json_expr '.workspace.default_model.model == "gpt-4o"' "Workspace GET carries the default model pair"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}" '{"default_model":{"provider_id":"","model":"gpt-4o"}}'
assert_status "400" "Half-set default model pair is rejected (400)"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}" '{"default_model":{"provider_id":"00000000-0000-0000-0000-000000000000","model":"gpt-4o"}}'
assert_status "400" "Unknown default model provider is rejected (400)"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" '{"name":"Inherit Agent","slug":"inherit-agent","role":"Tester","description":"Inherits the default","brief":"A short brief"}'
assert_status "201" "Inherit agent (empty provider/model pair) creates while a default exists"
assert_json_expr '.agent.provider_id == "" and .agent.model == ""' "Inherit agent persists with the empty pair"
INHERIT_AGENT_ID=$(json_get '.agent.id')

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}" '{"default_model":{"provider_id":"","model":""}}'
assert_status "422" "Clearing the default is blocked while agents inherit it (422)"
if [[ "${HTTP_BODY}" == *"1 agent(s) inherit"* ]]; then
    log_pass "Clear refusal names the inheriting agent count"
else
    log_fail "Clear refusal does not name the inheriting count: ${HTTP_BODY}"
fi

api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/agents/${INHERIT_AGENT_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Inherit agent deleted"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}" '{"default_model":{"provider_id":"","model":""}}'
assert_status "200" "Clearing the default succeeds once no agents inherit it"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${CHARLIE_TOKEN}"
assert_status "200" "Workspace read after clear returns 200"
assert_json_expr '.workspace.default_model == null' "Workspace payload carries null default model after clear"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/providers/${OPENAI_PROV_ID}/models" "${CHARLIE_TOKEN}"
assert_status "200" "List models from provider"
assert_json_expr 'has("models")' "Models endpoint returns models list"

api_req "POST" "/api/v1/providers/models-preview" "${CHARLIE_TOKEN}" '{"type":"openai","key":"sk-fake"}'
assert_status "200" "Models preview endpoint returns 200"

# 13.2b Workspace tool settings: catalog, gate toggle, validation, secrets
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/tools" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists workspace tools"
assert_json_expr '(.tools | map(.key) | index("ls")) != null' "Tool catalog contains fs tool ls"
assert_json_expr '(.tools | map(.key) | index("web.search")) != null' "Tool catalog contains web.search"
assert_json_expr '(.tools | map(select(.key == "browser")) | length) == 1' "Tool catalog exposes the single browser alias"
# document.* family (add-document-read-tool / add-document-create-tool):
# both verbs catalog under the document group behind the family seam.
assert_json_expr '(.tools | map(.key) | index("document.read")) != null' "Tool catalog contains document.read"
assert_json_expr '(.tools | map(select(.key == "document.read")) | .[0].group) == "document"' "document.read catalogs under group document"
assert_json_expr '(.tools | map(.key) | index("document.create")) != null' "Tool catalog contains document.create"
assert_json_expr '(.tools | map(select(.key == "document.create")) | .[0].group) == "document"' "document.create catalogs under group document"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/ls" "${CLI_USER_TOKEN}" '{"enabled":false}'
assert_status "403" "Member cannot change tool settings"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/ls" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "200" "Owner disables a tool"
assert_json_expr '.tool.enabled == false' "Disable is reflected in the response"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/tools" "${CHARLIE_TOKEN}"
assert_json_expr '.tools | map(select(.key == "ls")) | .[0].enabled == false' "List shows the tool disabled"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/ls" "${CHARLIE_TOKEN}" '{"enabled":true}'
assert_status "200" "Owner re-enables the tool"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/not.a.tool" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "400" "Unknown tool key returns 400"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/web.search" "${CHARLIE_TOKEN}" '{"enabled":true}'
assert_status "422" "Enabling web.search without provider entries returns 422"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/web.search" "${CHARLIE_TOKEN}" '{"enabled":true,"config":{"entries":[{"name":"Tavily 1","provider":"tavily"}]}}'
assert_status "422" "Entry missing its required credential returns 422"
assert_json_expr '[.error.details[]? | select(.field == "entries[0].api_key")] | length > 0' "Credential error fields the offending entry"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/web.search" "${CHARLIE_TOKEN}" '{"enabled":true,"config":{"entries":[{"name":"Tavily 1","provider":"tavily","api_key":"tvly-smoke-secret-key-9876"}]}}'
assert_status "200" "Configured provider stack enables"
assert_json_expr '.tool.configured == true' "Configured tool reports configured"
assert_json_expr '.tool.config.entries[0].api_key_hint == "9876"' "Entry credential carries only its last-4 hint"
if echo "${HTTP_BODY}" | grep -q "tvly-smoke-secret-key-9876"; then
    log_fail "Tool config response echoed the secret"
else
    log_pass "Tool config response never echoes the secret"
fi

# Always-on channel tools: the catalog marks the three channel tools
# non-toggleable (always-on) while ordinary tools stay toggleable, and
# PATCHing enabled on one is rejected instead of stored.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/tools" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists workspace tools for toggleability"
assert_json_expr '.tools | map(select(.key == "channel.post")) | .[0].toggleable == false' "channel.post is non-toggleable (always-on)"
assert_json_expr '.tools | map(select(.key == "channel.history")) | .[0].toggleable == false' "channel.history is non-toggleable (always-on)"
assert_json_expr '.tools | map(select(.key == "session.close")) | .[0].toggleable == false' "session.close is non-toggleable (always-on)"
assert_json_expr '.tools | map(select(.key == "ls")) | .[0].toggleable == true' "Ordinary tool ls stays toggleable"
assert_json_expr '.tools | map(select(.key == "web.search")) | .[0].toggleable == true' "Ordinary tool web.search stays toggleable"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/channel.post" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "422" "Disabling an always-on channel tool is rejected"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/tools" "${CHARLIE_TOKEN}"
assert_json_expr '.tools | map(select(.key == "channel.post")) | .[0].enabled == true' "Rejected patch left the channel tool enabled"

# 13.3 Delete in-use provider 409 (the agent references the mock provider)
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/providers/${MOCK_PROV_ID}" "${CHARLIE_TOKEN}"
assert_status "409" "Deleting in-use provider returns 409"

# 13.4 Memory endpoints (agent-memory): own user memory is membership-only and
# self-scoped to the caller; shared workspace memory reads ride membership and
# PUT requires workspace settings management (workspace.write).
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/me/memory" "${DAVE_TOKEN}"
assert_status "200" "Member reads own (absent) user memory"
assert_json_expr '.content == "" and .max_chars == 32000 and .updated_at == null' "Absent user memory is empty with the char budget"

api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/me/memory" "${DAVE_TOKEN}" '{"content":"Dave prefers concise answers."}'
assert_status "200" "Member PUTs own user memory"
assert_json_expr '.content == "Dave prefers concise answers." and .max_chars == 32000' "PUT echoes the saved shape"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/me/memory" "${DAVE_TOKEN}"
assert_status "200" "Member GETs own user memory back"
assert_json_expr '.content == "Dave prefers concise answers."' "User memory content round-trips"
assert_json_expr '.updated_at != null' "Saved user memory carries a timestamp"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/me/memory" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads their own user memory"
assert_json_expr '.content == ""' "User memory is self-scoped (owner sees own empty document)"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/memory" "${DAVE_TOKEN}"
assert_status "200" "Member reads shared workspace memory"
assert_json_expr '.content == ""' "Workspace memory starts empty"

api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/memory" "${DAVE_TOKEN}" '{"content":"member attempted write"}'
assert_status "403" "Member cannot write shared workspace memory (403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/memory" "${BOB_TOKEN}" '{"content":"# Team memory — smoke writes go through admins."}'
assert_status "200" "Admin PUTs shared workspace memory"
assert_json_expr '.content == "# Team memory — smoke writes go through admins." and .max_chars == 32000' "Admin PUT echoes the saved shape"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/memory" "${DAVE_TOKEN}"
assert_status "200" "Member reads the admin's workspace memory"
assert_json_expr '.content == "# Team memory — smoke writes go through admins."' "Workspace memory content round-trips"

# 13.4a Over-cap content is rejected with 422 naming the limit.
python3 -c 'import json,sys; open(sys.argv[1],"w").write(json.dumps({"content":"x"*32001}))' "${TMP_DIR}/overcap.json"
api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/me/memory" "${DAVE_TOKEN}" "$(cat "${TMP_DIR}/overcap.json")"
assert_status "422" "Over-cap user memory is rejected (422)"
assert_json_expr '.error.message | contains("32000")' "422 error names the char cap"

api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/memory" "${BOB_TOKEN}" "$(cat "${TMP_DIR}/overcap.json")"
assert_status "422" "Over-cap workspace memory is rejected (422)"

# 13.4b The old per-agent memory routes are gone (agent-memory design D8).
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/memory" "${CHARLIE_TOKEN}"
assert_status "404" "Old GET agent-memory route is gone (404)"

api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/memory" "${CHARLIE_TOKEN}"
assert_status "404" "Old DELETE agent-memory route is gone (404)"

# 13.5 Regenerate: enhance mode against the mock provider. On success the
# previous documents must survive as .bak beside the enhanced files.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/regenerate" "${CHARLIE_TOKEN}"
assert_status "200" "Regenerate returns 200"
assert_json_expr '.agent.prompts_status == "ready"' "Regenerate completes with ready status"
for doc in IDENTITY.md SOUL.md BOOTSTRAP.md; do
    if [[ -f "${AGENT_WS_DIR}/${doc}" ]]; then
        log_pass "Regenerated document present: ${doc}"
    else
        log_fail "Regenerated document missing: ${AGENT_WS_DIR}/${doc}"
    fi
done
if [[ -f "${AGENT_WS_DIR}/IDENTITY.md.bak" ]]; then
    log_pass "Previous generation preserved as IDENTITY.md.bak"
else
    log_fail "Backup IDENTITY.md.bak missing after regeneration"
fi

# 13.5b Regenerate with a change instruction — the current documents ride along
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/regenerate" "${CHARLIE_TOKEN}" '{"instruction":"make every section terser"}'
assert_status "200" "Regenerate with change instruction returns 200"
assert_json_expr '.agent.prompts_status == "ready"' "Instruction regeneration completes ready"

# 13.6 Atomic Workspace Birth Flow
NEW_TENANT_SLUG="birth-tenant-${RUN_ID}"
api_req "POST" "/api/v1/workspaces" "${CHARLIE_TOKEN}" '{"name":"Birth Tenant","slug":"'"${NEW_TENANT_SLUG}"'","provider":{"type":"openai","name":"OpenAI","key":"sk-smoke-secret-key-1234"},"starter_agent":{"name":"Starter Agent","slug":"starter","role":"Helper","description":"Helps out","brief":"A short brief","model":"gpt-4"}}'
assert_status "201" "Atomic workspace birth with provider and agent"
assert_json_expr 'has("workspace")' "Response has workspace"
assert_json_expr 'has("provider")' "Response has provider"
assert_json_expr 'has("agent")' "Response has agent"

# -----------------------------------------------------------------------------
# 14. /v1 OpenResponses Live Chat Sessions
# -----------------------------------------------------------------------------
log_step "14. /v1 OpenResponses Live Chat Sessions"

# 14.1 Mint a chat key through the existing key machinery: a plain Member
# exchanges for a workspace-scoped key (returned in plaintext exactly once).
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/api-keys/exchange" "${DAVE_TOKEN}"
assert_status "201" "Member exchanges a workspace-scoped chat key"
V1_KEY=$(json_get '.key')
if [[ -z "${V1_KEY}" || "${V1_KEY}" == "null" ]]; then
    log_fail "Failed to retrieve chat key from exchange response"
fi
log_pass "Chat key exchanged (${V1_KEY:0:11}...)"

# 14.2 The chat key authenticates /v1 and lists the workspace agents as models.
api_req "GET" "/v1/models" "${V1_KEY}"
assert_status "200" "GET /v1/models with the exchanged chat key"
assert_json_expr '([.data[].id] | index("test-agent")) != null' "Agent slug listed as a /v1 model"

# 14.3 Birth turn: an unknown metadata.onclaw_session births a persistent
# session in the key's workspace (streaming, the web binding style).
V1_SESSION="sess-smoke-$(date +%s)-${RANDOM}"
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE hello birth","stream":true,"metadata":{"onclaw_session":"'"${V1_SESSION}"'"}}'
assert_status "200" "Birth turn (streaming, metadata.onclaw_session) accepted"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.created"' || log_fail "Birth stream missing response.created"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.completed"' || log_fail "Birth stream missing response.completed"
printf '%s' "${HTTP_BODY}" | grep -q '^data: \[DONE\]' || log_fail "Birth stream missing [DONE] sentinel"
printf '%s' "${HTTP_BODY}" | grep -q 'live reply' || log_fail "Birth stream carried no assistant text delta"
log_pass "Birth turn streamed the full SSE lifecycle (assistant deltas included)"
V1_RESP_1=$(printf '%s' "${HTTP_BODY}" | grep -o '"id":"resp_[^"]*"' | head -1 | cut -d'"' -f4)
if [[ -z "${V1_RESP_1}" ]]; then
    log_fail "Birth stream carried no minted response id: ${HTTP_BODY}"
fi
log_pass "Birth turn minted response id: ${V1_RESP_1:0:24}..."

# 14.4 Chained turn: previous_response_id binds to the birth session
# (OpenResponses convention) and appends to the same history.
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE chained follow-up","previous_response_id":"'"${V1_RESP_1}"'"}'
assert_status "200" "Chained turn (previous_response_id) accepted"
assert_json_expr '.status == "completed"' "Chained turn completed"
assert_json_expr ".id | startswith(\"resp_${V1_SESSION}_\")" "Chained response id encodes the birth session"

# 14.5 Chaining cannot bootstrap: a well-formed previous_response_id to an
# unknown session is not-found and never births.
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE ghost","previous_response_id":"resp_sess-ghost-unknown_00000000-0000-0000-0000-000000000000"}'
assert_status "404" "Chaining against an unknown session returns 404 (bind-only, never births)"

# 14.6 The born session's events are retrievable on the native session-events
# endpoint (workspace JWT auth): birth turn and chained turn both persisted.
# The assistant appends land shortly after the turn's HTTP response returns,
# so poll briefly for the full history before asserting.
for i in {1..20}; do
    api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions/${V1_SESSION}/events?limit=100" "${CHARLIE_TOKEN}"
    assert_status "200" "Native session-events endpoint serves the born session"
    if [[ "$(json_get '(.events | length)')" -ge 4 ]]; then
        break
    fi
    sleep 0.25
done
assert_json_expr '(.events | length) >= 4' "Birth + chained turns persisted events (user and assistant per turn)"
assert_json_expr '[.events[]?.message.content // "" ] | join(" ") | contains("ONCLAW_V1_SMOKE hello birth")' "Birth turn input present in session history"
assert_json_expr '[.events[]?.message.content // "" ] | join(" ") | contains("ONCLAW_V1_SMOKE chained follow-up")' "Chained turn input present in session history"

# -----------------------------------------------------------------------------
# 15. MCP Server Registry, Probes, Agent-Private Servers & enabled_mcps
# -----------------------------------------------------------------------------
log_step "15. MCP Server Registry, Probes & enabled_mcps"

# 15.0 Compile the stdio stub MCP server — the same testdata binary the handler
# tests drive: a real mcp-go server exposing exactly 3 tools over stdio
# (test_tool, echo_env, echo_args).
MCP_STUB_BIN="${TMP_DIR}/mockmcpserver"
go build -o "${MCP_STUB_BIN}" ./internal/agents/mcp/testdata/mockmcpserver || {
    log_fail "Failed to compile the stub MCP server (internal/agents/mcp/testdata/mockmcpserver)"
}
log_pass "Stub MCP server compiled from internal/agents/mcp/testdata/mockmcpserver"

MCP_BASE="/api/v1/workspaces/${TENANT_SLUG}/mcp-servers"
MCP_SECRET_NAME="ONCLAW_MCP_TEST_MARKER"
MCP_SECRET_VALUE="onclaw-smoke-secret-9876"

# 15.1 Permission guards: a Member can read the registry (tools.read) but
# cannot register servers (tools.write).
api_req "GET" "${MCP_BASE}" "${CLI_USER_TOKEN}"
assert_status "200" "Member can list MCP servers (tools.read)"
assert_json_expr '(.servers | length) == 0' "MCP registry starts empty"

api_req "POST" "${MCP_BASE}" "${CLI_USER_TOKEN}" '{"name":"Nope","transport":"stdio","command":"x"}'
assert_status "403" "Member cannot create MCP servers (tools.write 403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

# 15.2 Owner registers a stdio server against the stub; the on-save probe
# connects and counts the stub's 3 tools; the env secret round-trips as a
# hint only — never plaintext.
api_req "POST" "${MCP_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Smoke Stub\",\"transport\":\"stdio\",\"command\":\"${MCP_STUB_BIN}\",\"args\":[\"--marker=smoke\"],\"env\":[{\"name\":\"${MCP_SECRET_NAME}\",\"value\":\"${MCP_SECRET_VALUE}\"}]}"
assert_status "201" "Owner registers a stdio MCP server (201)"
assert_json_expr '.server.transport == "stdio"' "Create echoes the transport"
assert_json_expr '.server.command == "'"${MCP_STUB_BIN}"'"' "Create echoes the command"
assert_json_expr '.server.status == "connected"' "On-save probe connects to the stub"
assert_json_expr '.server.tool_count == 3' "Probe counts the stub's 3 tools"
assert_json_expr '.server.enabled == true' "Servers default to enabled"
assert_json_expr '.server.env[0].name == "'"${MCP_SECRET_NAME}"'"' "Env row echoed by name"
assert_json_expr '.server.env[0].value_hint == "9876"' "Env secret carries only its last-4 hint"
if echo "${HTTP_BODY}" | grep -q "${MCP_SECRET_VALUE}"; then
    log_fail "MCP create response leaked the plaintext env secret"
else
    log_pass "MCP create response never echoes the env secret"
fi
MCP_SRV_ID=$(json_get '.server.id')

# 15.3 Fielded validation: stdio without a command, an unknown transport, and
# a (case-insensitive) duplicate name are all 422s.
api_req "POST" "${MCP_BASE}" "${CHARLIE_TOKEN}" '{"name":"Broken Stdio","transport":"stdio"}'
assert_status "422" "stdio server without command is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "command")] | length > 0' "Validation error fields command"

api_req "POST" "${MCP_BASE}" "${CHARLIE_TOKEN}" '{"name":"Broken Transport","transport":"carrier_pigeon","command":"x"}'
assert_status "422" "Unknown transport is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "transport")] | length > 0' "Validation error fields transport"

api_req "POST" "${MCP_BASE}" "${CHARLIE_TOKEN}" '{"name":"smoke stub","transport":"stdio","command":"x"}'
assert_status "422" "Duplicate server name (case-insensitive) is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "name")] | length > 0' "Duplicate-name error fields name"

# 15.4 The registry lists the server with its persisted probe status.
api_req "GET" "${MCP_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists MCP servers"
assert_json_expr "[.servers[] | select(.id == \"${MCP_SRV_ID}\")] | length == 1" "Registry lists the registered server"
assert_json_expr "[.servers[] | select(.id == \"${MCP_SRV_ID}\")][0].status == \"connected\"" "Connected status persists on the row"
assert_json_expr "[.servers[] | select(.id == \"${MCP_SRV_ID}\")][0].tool_count == 3" "Tool count persists on the row"

# 15.5 PATCH: change the args; then keep the stored secret via an empty env
# value (name-keyed merge); the master switch pauses/resumes without touching
# the connection.
api_req "PATCH" "${MCP_BASE}/${MCP_SRV_ID}" "${CHARLIE_TOKEN}" '{"args":["--marker=smoke-patched"]}'
assert_status "200" "Owner patches the server args"
assert_json_expr '.server.args[0] == "--marker=smoke-patched"' "Patched args are echoed"
assert_json_expr '.server.status == "connected"' "Patched server re-probes connected"
assert_json_expr '.server.env[0].value_hint == "9876"' "Args-only patch keeps the stored secret hint"

api_req "PATCH" "${MCP_BASE}/${MCP_SRV_ID}" "${CHARLIE_TOKEN}" "{\"env\":[{\"name\":\"${MCP_SECRET_NAME}\",\"value\":\"\"}]}"
assert_status "200" "Owner patches with an empty env secret value"
assert_json_expr '.server.env[0].value_hint == "9876"' "Empty env value keeps the stored secret (hint unchanged)"

api_req "PATCH" "${MCP_BASE}/${MCP_SRV_ID}" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "200" "Owner pauses the server (master switch)"
assert_json_expr '.server.enabled == false' "Paused server reports enabled false"
assert_json_expr ".server.env[0].name == \"${MCP_SECRET_NAME}\"" "Master-switch patch keeps the connection intact"

api_req "PATCH" "${MCP_BASE}/${MCP_SRV_ID}" "${CHARLIE_TOKEN}" '{"enabled":true}'
assert_status "200" "Owner resumes the server"
assert_json_expr '.server.enabled == true' "Resumed server reports enabled true"

# 15.6 Explicit probe endpoint: a fresh bounded dial, persisted and returned.
api_req "POST" "${MCP_BASE}/${MCP_SRV_ID}/probe" "${CHARLIE_TOKEN}"
assert_status "200" "Owner probes the server on demand"
assert_json_expr '.server.status == "connected"' "Re-probe reports connected"
assert_json_expr '.server.tool_count == 3' "Re-probe counts 3 tools"

# 15.7 An unreachable command still persists (201) with the failed probe
# stored as the row status; the explicit re-probe behaves identically.
api_req "POST" "${MCP_BASE}" "${CHARLIE_TOKEN}" '{"name":"Dead Stub","transport":"stdio","command":"/nonexistent/onclaw-mcp-smoke-binary"}'
assert_status "201" "Registering an unreachable server still persists (201)"
assert_json_expr '.server.status == "error"' "Failed on-save probe persists error status"
assert_json_expr '.server.status_error != ""' "Probe failure message is persisted"
MCP_DEAD_ID=$(json_get '.server.id')

api_req "POST" "${MCP_BASE}/${MCP_DEAD_ID}/probe" "${CHARLIE_TOKEN}"
assert_status "200" "Re-probing the dead server returns 200"
assert_json_expr '.server.status == "error"' "Re-probe persists the error status"

# 15.8 Delete: 204, then the row is unreachable (probe 404) and gone from the
# registry.
api_req "DELETE" "${MCP_BASE}/${MCP_DEAD_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes the dead server (204)"
api_req "POST" "${MCP_BASE}/${MCP_DEAD_ID}/probe" "${CHARLIE_TOKEN}"
assert_status "404" "Probing a deleted server returns 404"
api_req "GET" "${MCP_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr "[.servers[] | select(.id == \"${MCP_DEAD_ID}\")] | length == 0" "Deleted server is gone from the registry"

# 15.9 Agent-private servers under /agents/:slug/mcp-servers (agents.write for
# writes): Member guard, attach against the stub, registry invisibility, list,
# delete.
MCP_AGENT_BASE="/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/mcp-servers"

api_req "POST" "${MCP_AGENT_BASE}" "${CLI_USER_TOKEN}" '{"name":"Nope Private","transport":"stdio","command":"x"}'
assert_status "403" "Member cannot attach private MCP servers (agents.write 403)"

api_req "POST" "${MCP_AGENT_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Agent Private Stub\",\"transport\":\"stdio\",\"command\":\"${MCP_STUB_BIN}\",\"env\":[{\"name\":\"PRIVATE_TOKEN\",\"value\":\"tok-private-4321\"}]}"
assert_status "201" "Owner attaches a private MCP server to the agent"
assert_json_expr ".server.agent_id == \"${AGENT_ID}\"" "Private server carries the agent id"
assert_json_expr '.server.status == "connected"' "Private server probe connects"
assert_json_expr '.server.tool_count == 3' "Private server probe counts 3 tools"
assert_json_expr '.server.env[0].value_hint == "4321"' "Private server secret hinted"
MCP_PRIV_ID=$(json_get '.server.id')

api_req "GET" "${MCP_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr "[.servers[] | select(.id == \"${MCP_PRIV_ID}\")] | length == 0" "Private server is absent from the workspace registry"

api_req "GET" "${MCP_AGENT_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the agent's private servers"
assert_json_expr "[.servers[] | select(.id == \"${MCP_PRIV_ID}\")] | length == 1" "Agent list shows the private server"

api_req "DELETE" "${MCP_AGENT_BASE}/${MCP_PRIV_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner removes the private server (204)"
api_req "GET" "${MCP_AGENT_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr "[.servers[] | select(.id == \"${MCP_PRIV_ID}\")] | length == 0" "Removed private server is gone"

# 15.10 Agent create echoes enabled_mcps (the opt-in allowlist of server
# UUIDs) and ignores the legacy disabled_mcps denylist in payloads.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" "{\"name\":\"MCP Optin Agent\",\"slug\":\"mcp-optin-agent\",\"role\":\"Tester\",\"description\":\"Opts into MCP\",\"brief\":\"A short brief\",\"provider_id\":\"${MOCK_PROV_ID}\",\"model\":\"gpt-4\",\"enabled_mcps\":[\"${MCP_SRV_ID}\"],\"disabled_mcps\":[\"legacy-denylist-server\"]}"
assert_status "201" "Owner creates an agent with enabled_mcps and a legacy disabled_mcps payload"
assert_json_expr ".agent.enabled_mcps == [\"${MCP_SRV_ID}\"]" "Agent create echoes enabled_mcps"
if echo "${HTTP_BODY}" | grep -q "disabled_mcps"; then
    log_fail "Agent response carries the legacy disabled_mcps field"
else
    log_pass "Agent responses never carry disabled_mcps"
fi

# -----------------------------------------------------------------------------
# 16. Agent Hooks: Workspace CRUD, Validation, Reorder, Dry-Run, Executions,
#     Agent-Level Hooks & Instance-Admin Managed Hooks
# -----------------------------------------------------------------------------
log_step "16. Agent Hooks: CRUD, Validation, Dry-Run, Admin Managed"

HOOKS_BASE="/api/v1/workspaces/${TENANT_SLUG}/hooks"
HOOK_SECRET_VALUE="Bearer onclaw-hook-secret-4321"

# 16.1 Permission guards: a plain Member holds neither hooks.read nor
# hooks.write (Member role is read-only on the catalog).
api_req "GET" "${HOOKS_BASE}" "${CLI_USER_TOKEN}"
assert_status "403" "Member cannot list hooks (hooks.read 403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

api_req "POST" "${HOOKS_BASE}" "${CLI_USER_TOKEN}" '{"name":"Nope","event":"run_finished","handler_type":"http","config":{"url":"https://example.com/hook"}}'
assert_status "403" "Member cannot create hooks (hooks.write 403)"

# 16.2 Save validation: an un-compilable regex matcher string is a fielded 422.
api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Broken Regex","event":"pre_tool_use","matcher":"(","handler_type":"http","config":{"url":"https://example.com/hook"}}'
assert_status "422" "Un-compilable regex matcher is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "matcher")] | length > 0' "Validation error fields matcher"

# 16.3 Owner creates an http hook with a secret header: 201 with the
# save-time match count and a masked (last-4) hint — never the plaintext.
api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Policy Gate\",\"event\":\"pre_tool_use\",\"matcher\":\"web.fetch\",\"handler_type\":\"http\",\"config\":{\"url\":\"https://hooks.example.com/onclaw\",\"headers\":[{\"name\":\"Authorization\",\"value\":\"${HOOK_SECRET_VALUE}\"}]}}"
assert_status "201" "Owner creates an http hook with a secret header"
assert_json_expr '.hook.handler_type == "http"' "Create echoes the handler type"
assert_json_expr '.hook.matcher == "web.fetch"' "Create echoes the matcher string"
assert_json_expr '.match_count.matched >= 1 and .match_count.of > .match_count.matched' "Match count reports matched of visible tools"
assert_json_expr '.hook.config.headers[0].value == "4321"' "Secret header carries only its last-4 hint"
if echo "${HTTP_BODY}" | grep -q "onclaw-hook-secret-4321"; then
    log_fail "Hook create response leaked the plaintext header secret"
else
    log_pass "Hook create response never echoes the secret"
fi
HOOK_ID=$(json_get '.hook.id')

# 16.4 Duplicate hook name is a 409 conflict.
api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Policy Gate","event":"run_finished","handler_type":"http","config":{"url":"https://hooks.example.com/other"}}'
assert_status "409" "Duplicate hook name returns 409"

# 16.5 The workspace list carries the read-only (empty) instance section.
api_req "GET" "${HOOKS_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists hooks"
assert_json_expr '(.hooks | length) == 1' "Workspace list shows the created hook"
assert_json_expr '(.instance | length) == 0' "Instance section starts empty"

# 16.6 PATCH with the hint echoed back keeps the stored secret (hint is
# unchanged); the rename rides along.
api_req "PATCH" "${HOOKS_BASE}/${HOOK_ID}" "${CHARLIE_TOKEN}" '{"name":"Policy Gate Renamed","config":{"url":"https://hooks.example.com/onclaw","headers":[{"name":"Authorization","value":"4321"}]}}'
assert_status "200" "Owner patches the hook echoing the secret hint"
assert_json_expr '.hook.name == "Policy Gate Renamed"' "Patch renamed the hook"
assert_json_expr '.hook.config.headers[0].value == "4321"' "Hint-echo patch keeps the stored secret"

# 16.7 Reorder: add a second hook and move it to the front (D14: the list
# order is the execution order).
api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Cheap First","event":"pre_tool_use","handler_type":"http","config":{"url":"https://hooks.example.com/cheap"}}'
assert_status "201" "Owner creates a second hook"
HOOK_SECOND_ID=$(json_get '.hook.id')
api_req "POST" "${HOOKS_BASE}/reorder" "${CHARLIE_TOKEN}" "{\"ids\":[\"${HOOK_SECOND_ID}\",\"${HOOK_ID}\"]}"
assert_status "204" "Owner reorders hooks (204)"
api_req "GET" "${HOOKS_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr '.hooks[0].name == "Cheap First"' "Reorder moves the second hook to the front"

# 16.8 Execution history starts empty for the hook.
api_req "GET" "${HOOKS_BASE}/${HOOK_ID}/executions" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists hook executions"
assert_json_expr '(.executions | length) == 0' "Execution history starts empty"

# 16.9 Test dry-run (command handler): exit 0 is an allow with the exit code
# in the detail, and no audit row is written (16.8's count stays 0).
api_req "POST" "${HOOKS_BASE}/test" "${CHARLIE_TOKEN}" '{"name":"Dry Run Gate","event":"pre_tool_use","handler_type":"command","config":{"command":"/bin/sh","args":["-c","cat >/dev/null; exit 0"]}}'
assert_status "200" "Dry-run executes the real command handler"
assert_json_expr '.decision == "allow"' "Exit-0 command dry-run allows"
assert_json_expr '.detail.exit_code == 0' "Dry-run detail carries the exit code"
assert_json_expr 'has("duration_ms")' "Dry-run reports its duration"

api_req "GET" "${HOOKS_BASE}/${HOOK_ID}/executions" "${CHARLIE_TOKEN}"
assert_json_expr '(.executions | length) == 0' "Dry-run writes no audit row"

# 16.10 Exit 2 with stderr blocks with the (trimmed) stderr reason — the
# Claude Code compatibility row of the decision table.
api_req "POST" "${HOOKS_BASE}/test" "${CHARLIE_TOKEN}" '{"name":"Dry Run Gate","event":"pre_tool_use","handler_type":"command","config":{"command":"/bin/sh","args":["-c","echo blocked by smoke policy >&2; exit 2"]}}'
assert_status "200" "Exit-2 command dry-run completes"
assert_json_expr '.decision == "block"' "Exit-2 command dry-run blocks"
assert_json_expr '.reason == "blocked by smoke policy"' "Exit-2 reason is the trimmed stderr"

# 16.11 Agent-level hooks (agent config modal): create a private hook on
# test-agent, see the three-section listing, then remove it.
AGENT_HOOKS_BASE="/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/hooks"

api_req "POST" "${AGENT_HOOKS_BASE}" "${CLI_USER_TOKEN}" '{"name":"Nope Private","event":"pre_tool_use","handler_type":"command","config":{"command":"/bin/echo"}}'
assert_status "403" "Member cannot create agent hooks (hooks.write 403)"

api_req "POST" "${AGENT_HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Atlas Gate","event":"pre_tool_use","matcher":"execute","if":"execute(command)","handler_type":"command","config":{"command":"/bin/echo"}}'
assert_status "201" "Owner creates an agent-private hook"
assert_json_expr '.match_count.matched >= 1' "Agent hook match count is reported"
assert_json_expr '.hook.matcher == "execute" and .hook["if"] == "execute(command)"' "String matcher and if condition round-trip"
AGENT_HOOK_ID=$(json_get '.hook.id')

api_req "GET" "${AGENT_HOOKS_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the agent's hook sections"
assert_json_expr '(.agent | length) == 1' "Agent section shows the private hook"
assert_json_expr '(.instance | length) == 0 and (.workspace | length) == 2' "Instance and workspace sections are read-only visibility"

api_req "PATCH" "${AGENT_HOOKS_BASE}/${AGENT_HOOK_ID}" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "200" "Owner disables the agent-private hook"
assert_json_expr '.hook.enabled == false' "Disable is reflected"

api_req "DELETE" "${AGENT_HOOKS_BASE}/${AGENT_HOOK_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes the agent-private hook (204)"

# 16.12 Instance-admin surface: the tenant Owner is not a master-tenant
# member, so RequireMasterWorkspace's enumeration defense hides the surface
# behind the same 404 "workspace not found" every other admin route returns
# (section 7 convention); the superadmin manages managed rows and sees the
# (empty) builtin listing.
api_req "GET" "/api/v1/admin/hooks" "${CHARLIE_TOKEN}"
assert_status "404" "Tenant owner cannot see the instance-admin hooks surface (404)"

api_req "GET" "/api/v1/admin/hooks" "${SUPERADMIN_TOKEN}"
assert_status "200" "Superadmin lists instance hooks"
assert_json_expr '(.builtin | length) == 0' "Builtin listing starts empty (v1 ships none)"
assert_json_expr '(.managed | length) == 0' "Managed listing starts empty"

api_req "POST" "/api/v1/admin/hooks" "${SUPERADMIN_TOKEN}" '{"key":"smoke-org-gate","name":"Org Policy Gate","event":"run_finished","handler_type":"http","config":{"url":"https://policy.example.com/hook"}}'
assert_status "201" "Superadmin creates a managed instance hook"
assert_json_expr '.hook.source == "managed"' "Managed row carries source managed"
assert_json_expr '.hook.version == 1' "Managed row starts at version 1"
ADMIN_HOOK_ID=$(json_get '.hook.id')

api_req "POST" "/api/v1/admin/hooks" "${SUPERADMIN_TOKEN}" '{"key":"smoke-org-gate","name":"Dup","event":"run_finished","handler_type":"http","config":{"url":"https://policy.example.com/other"}}'
assert_status "409" "Duplicate instance hook key returns 409"

api_req "PATCH" "/api/v1/admin/hooks/${ADMIN_HOOK_ID}" "${SUPERADMIN_TOKEN}" '{"name":"Renamed Org Gate"}'
assert_status "200" "Superadmin patches the managed instance hook"
assert_json_expr '.hook.name == "Renamed Org Gate"' "Managed rename is reflected"

api_req "DELETE" "/api/v1/admin/hooks/${ADMIN_HOOK_ID}" "${SUPERADMIN_TOKEN}"
assert_status "204" "Superadmin deletes the managed instance hook (204)"

# 16.13 Workspace cleanup: delete the reordered hook, then 404 afterwards.
api_req "DELETE" "${HOOKS_BASE}/${HOOK_SECOND_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes a workspace hook (204)"
api_req "PATCH" "${HOOKS_BASE}/${HOOK_SECOND_ID}" "${CHARLIE_TOKEN}" '{"name":"Ghost"}'
assert_status "404" "Patching a deleted hook returns 404"

# 16.14 Script handler (D22): save-time compile validation fields config.script
# with the first syntax error's line/column; a valid script saves with a
# non-match-all matcher (script hooks carry no matcher requirement); the
# dry-run executes a block script live and surfaces the captured console
# output beside the blocked decision.
api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Broken Script","event":"pre_tool_use","handler_type":"script","config":{"script":"(function(input){ const bad = ; })"}}'
assert_status "422" "Syntax-error script is rejected at save (422)"
assert_json_expr '[.error.details[]? | select(.field == "config.script")] | length > 0' "Script validation error fields config.script"
assert_json_expr '.error.details[0].message | test("line [0-9]+, column [0-9]+")' "Script error message names line and column"

api_req "POST" "${HOOKS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Script Gate","event":"pre_tool_use","matcher":"shell","handler_type":"script","config":{"script":"(function(input){ return { decision: \"allow\" }; })"}}'
assert_status "201" "Owner creates a script hook (no matcher restriction applies)"
assert_json_expr '.hook.handler_type == "script"' "Create echoes the script handler type"
assert_json_expr '.hook.config.script | contains("decision")' "Script source round-trips in the view"

api_req "POST" "${HOOKS_BASE}/test" "${CHARLIE_TOKEN}" '{"name":"Script Dry Run","event":"pre_tool_use","handler_type":"script","config":{"script":"(function(input){ console.log(\"script gate\"); return { decision: \"block\", reason: \"blocked by script\" }; })"}}'
assert_status "200" "Script dry-run executes the real script handler"
assert_json_expr '.decision == "block"' "Script dry-run blocks on the block return"
assert_json_expr '.reason == "blocked by script"' "Script block reason is honored"
assert_json_expr '.detail.console_lines | index("script gate") != null' "Dry-run detail carries the captured console output"



# -----------------------------------------------------------------------------
# 17. Channels: CRUD, Membership, Feed & SSE (integrate-agent-channels)
# -----------------------------------------------------------------------------
log_step "17. Channels: CRUD, Membership, Feed & SSE"

CHANNELS_BASE="/api/v1/workspaces/${TENANT_SLUG}/channels"

# 17.1 Permission guards: the builtin Member role holds neither channels.read
# nor channels.write, so a plain Member is 403 on the whole surface.
api_req "GET" "${CHANNELS_BASE}" "${CLI_USER_TOKEN}"
assert_status "403" "Member cannot list channels (channels.read 403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

api_req "POST" "${CHANNELS_BASE}" "${CLI_USER_TOKEN}" '{"name":"Nope","slug":"nope"}'
assert_status "403" "Member cannot create channels (channels.write 403)"

# 17.2 Owner creates the room; the slug is the #handle and URL form.
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Production Ops","slug":"ops","purpose":"Coordinate production incident response.","conventions":"Keep runbooks linked. One incident per chain."}'
assert_status "201" "Owner creates channel #ops"
assert_json_expr '.channel.slug == "ops"' "Create echoes the slug"
assert_json_expr '.channel.name == "Production Ops"' "Create echoes the name"
CHANNEL_ID=$(json_get '.channel.id')

# 17.3 Slug conflicts are 409 (workspace-scoped). The store's uniqueness is
# case-insensitive, but kebab-case validation is lowercase-only, so a
# case-variant slug is rejected as invalid input (422) before the conflict
# check can apply.
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Ops Duplicate","slug":"ops"}'
assert_status "409" "Duplicate channel slug returns 409"
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Ops Uppercase","slug":"OPS"}'
assert_status "422" "Case-variant slug OPS is invalid input (kebab-case is lowercase-only, 422)"

# 17.4 Validation: blank name and an invalid slug are fielded 422s.
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"  ","slug":"Bad Slug!"}'
assert_status "422" "Blank name + invalid slug is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "name")] | length > 0' "Validation error fields name"
assert_json_expr '[.error.details[]? | select(.field == "slug")] | length > 0' "Validation error fields slug"

# 17.5 Reads: list, get by id, get by slug, and the enumeration defense for
# non-members of the workspace.
api_req "GET" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists channels"
assert_json_expr "[.channels[] | select(.id == \"${CHANNEL_ID}\")] | length == 1" "List shows #ops"

api_req "GET" "${CHANNELS_BASE}/${CHANNEL_ID}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner gets the channel by id"
api_req "GET" "${CHANNELS_BASE}/ops" "${CHARLIE_TOKEN}"
assert_status "200" "Owner gets the channel by slug"
assert_json_expr ".channel.id == \"${CHANNEL_ID}\"" "Slug lookup resolves the same channel"

api_req "GET" "/api/v1/workspaces/master/channels" "${CHARLIE_TOKEN}"
assert_status "404" "Non-member of the workspace gets 404 (enumeration defense)"

# 17.6 PATCH covers name/purpose/conventions; the slug is identity, not patch.
api_req "PATCH" "${CHANNELS_BASE}/${CHANNEL_ID}" "${CHARLIE_TOKEN}" '{"purpose":"Incident coordination for the smoke tenant."}'
assert_status "200" "Owner patches the channel purpose"
assert_json_expr '.channel.purpose == "Incident coordination for the smoke tenant."' "Patched purpose round-trips"
assert_json_expr '.channel.slug == "ops"' "PATCH leaves the slug untouched"

# 17.7 Membership: an agent joins with a specialization; duplicates are 409;
# the roster resolves display name + @handle.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" "{\"name\":\"Ops Agent\",\"slug\":\"ops-agent\",\"role\":\"Responder\",\"description\":\"Channels smoke agent\",\"brief\":\"A short brief\",\"provider_id\":\"${MOCK_PROV_ID}\",\"model\":\"gpt-4\"}"
assert_status "201" "Owner creates the channel agent (generation runs against the mock provider)"
OPS_AGENT_ID=$(json_get '.agent.id')

api_req "POST" "${CHANNELS_BASE}/${CHANNEL_ID}/members" "${CHARLIE_TOKEN}" "{\"member_type\":\"agent\",\"agent_id\":\"${OPS_AGENT_ID}\",\"specialization\":\"Metrics and dashboards\"}"
assert_status "201" "Owner adds the agent with a specialization"
assert_json_expr '.member.member_type == "agent"' "Roster row carries member_type agent"
assert_json_expr '.member.display_name == "Ops Agent"' "Roster resolves the agent display name"
assert_json_expr '.member.handle == "ops-agent"' "Roster resolves the agent handle (slug)"
assert_json_expr '.member.specialization == "Metrics and dashboards"' "Specialization round-trips"
CHANNEL_MEMBER_ID=$(json_get '.member.id')

api_req "POST" "${CHANNELS_BASE}/${CHANNEL_ID}/members" "${CHARLIE_TOKEN}" "{\"member_type\":\"agent\",\"agent_id\":\"${OPS_AGENT_ID}\"}"
assert_status "409" "Re-adding the same agent is a 409 duplicate"

api_req "GET" "${CHANNELS_BASE}/${CHANNEL_ID}/members" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the roster"
assert_json_expr '(.members | length) == 1' "Roster has one member"

api_req "PATCH" "${CHANNELS_BASE}/${CHANNEL_ID}/members/${CHANNEL_MEMBER_ID}" "${CHARLIE_TOKEN}" '{"specialization":"Stakeholder comms"}'
assert_status "200" "Owner patches the specialization"
assert_json_expr '.member.specialization == "Stakeholder comms"' "Patched specialization round-trips"

# 17.8 Feed: the owner posts a human message mentioning the agent. The
# ONCLAW_V1_SMOKE marker steers the section-13 mock provider into its
# plain-assistant-reply branch, so the deterministic summon (D3 tier 1) runs
# against the mock and the agent auto-posts its final reply into the feed (D9).
api_req "POST" "${CHANNELS_BASE}/${CHANNEL_ID}/messages" "${CHARLIE_TOKEN}" '{"body":"@ops-agent ONCLAW_V1_SMOKE summarize the channel conventions"}'
assert_status "201" "Owner posts a human message mentioning the agent"
assert_json_expr '.message.author_type == "user"' "Human message is user-authored"
assert_json_expr '.message.seq >= 1' "Message carries the store-assigned seq"
CHANNEL_MSG_SEQ=$(json_get '.message.seq')

# 17.9 The summoned agent's auto-post lands above the human cursor. Poll the
# cursor briefly — the run takes a few model hops against the mock.
AGENT_MSG_FOUND=0
for i in {1..40}; do
    api_req "GET" "${CHANNELS_BASE}/${CHANNEL_ID}/messages?after=${CHANNEL_MSG_SEQ}" "${CHARLIE_TOKEN}"
    assert_status "200" "Feed cursor serves messages after seq ${CHANNEL_MSG_SEQ}"
    if [[ "$(json_get '([.messages[] | select(.author_type == "agent")] | length)')" -ge 1 ]]; then
        AGENT_MSG_FOUND=1
        break
    fi
    sleep 0.5
done
if [[ "${AGENT_MSG_FOUND}" -ne 1 ]]; then
    log_fail "Agent auto-post never appeared above cursor ${CHANNEL_MSG_SEQ} (D9 auto-post path)"
fi
log_pass "Agent auto-posted its final reply into the feed"
assert_json_expr '[.messages[] | select(.author_type == "agent")][0].body | contains("live reply")' "Agent reply body comes from the mock provider"
assert_json_expr '[.messages[] | select(.author_type == "agent")][0].session_id | startswith("chan_")' "Agent reply links its deterministic channel session (D7)"

# An untagged message persists; the observe-decide fan-out stays silent on the
# mock (the decider gets a non-engaging response — soft-gate doctrine).
api_req "POST" "${CHANNELS_BASE}/${CHANNEL_ID}/messages" "${CHARLIE_TOKEN}" '{"body":"Untagged status note: all quiet on the ops front."}'
assert_status "201" "Untagged human message persists"

# 17.10 SSE: subscribe, post a canary, and watch the message_posted frame
# (with its feed seq for client-side dedup) arrive live.
SSE_FILE="${TMP_DIR}/channel_events.sse"
curl -s -N --max-time 15 -H "Authorization: Bearer ${CHARLIE_TOKEN}" "${SERVER_URL}${CHANNELS_BASE}/${CHANNEL_ID}/events?stream=true" > "${SSE_FILE}" 2>/dev/null &
SSE_PID=$!
sleep 1
api_req "POST" "${CHANNELS_BASE}/${CHANNEL_ID}/messages" "${CHARLIE_TOKEN}" '{"body":"SSE canary: live wire check"}'
assert_status "201" "Canary message posted while the SSE subscription is live"
SSE_OK=0
for i in {1..20}; do
    if grep -q '"type":"message_posted"' "${SSE_FILE}" 2>/dev/null; then
        SSE_OK=1
        break
    fi
    sleep 0.5
done
kill "${SSE_PID}" 2>/dev/null || true
wait "${SSE_PID}" 2>/dev/null || true
if [[ "${SSE_OK}" -ne 1 ]]; then
    log_fail "SSE stream carried no message_posted event. Captured: $(head -5 "${SSE_FILE}")"
fi
log_pass "SSE stream delivered the live message_posted event"
grep -q '"seq":' "${SSE_FILE}" || log_fail "message_posted frame carries no seq for client dedup"
log_pass "SSE frames carry seq for client-side dedup"

# 17.11 Cleanup: remove the agent, delete the room, confirm it is gone.
api_req "DELETE" "${CHANNELS_BASE}/${CHANNEL_ID}/members/${CHANNEL_MEMBER_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner removes the agent from the roster (204)"

api_req "DELETE" "${CHANNELS_BASE}/${CHANNEL_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes the channel (204)"

api_req "GET" "${CHANNELS_BASE}/${CHANNEL_ID}" "${CHARLIE_TOKEN}"
assert_status "404" "Deleted channel is gone (404)"

# -----------------------------------------------------------------------------
# 18. Teams: Templates, Materialization, Kickoff & Work Sessions (channel-teams)
# -----------------------------------------------------------------------------
log_step "18. Teams: Templates, Materialization & Work Sessions"

# 18.1 Fresh workspace for the teams leg: template-spawned agents bind the
# workspace's first provider, so the mock provider must be the only one there.
TEAMS_TENANT_SLUG="teams-tenant-${RUN_ID}"
TEAMS_CHANNEL_BASE="/api/v1/workspaces/${TEAMS_TENANT_SLUG}/channels"
api_req "POST" "/api/v1/workspaces" "${CHARLIE_TOKEN}" "{\"name\":\"Teams Tenant\",\"slug\":\"${TEAMS_TENANT_SLUG}\"}"
assert_status "201" "Charlie creates the dedicated teams workspace"

api_req "POST" "/api/v1/workspaces/${TEAMS_TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Teams Mock Provider","base_url":"http://127.0.0.1:'"${MOCK_PORT}"'/v1","key":"sk-mock-key"}'
assert_status "201" "Owner registers the mock provider as the teams workspace's only provider"

# 18.2 Permission guards: a plain Member holds neither channels.read nor
# channels.write on the teams surface. Role ids are workspace-scoped, so the
# Member role comes from the teams workspace's own role catalog.
api_req "GET" "/api/v1/workspaces/${TEAMS_TENANT_SLUG}/roles" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the teams workspace roles"
TEAMS_MEMBER_ROLE_ID=$(json_get '.roles[] | select(.name == "Member") | .id')

api_req "POST" "/api/v1/admin/workspaces/${TEAMS_TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${DAVE_EMAIL}\",\"role_id\":\"${TEAMS_MEMBER_ROLE_ID}\"}"
assert_status "201" "Dave joins the teams workspace as a Member"

api_req "GET" "${TEAMS_CHANNEL_BASE}/templates" "${DAVE_TOKEN}"
assert_status "403" "Member cannot list team templates (channels.read 403)"
api_req "POST" "${TEAMS_CHANNEL_BASE}/templates/software-team/materialize" "${DAVE_TOKEN}" '{"name":"Nope","slug":"nope"}'
assert_status "403" "Member cannot materialize templates (channels.write 403)"

# 18.3 Template listing: the built-in Software Team with its six slots.
api_req "GET" "${TEAMS_CHANNEL_BASE}/templates" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists team templates"
assert_json_expr '(.templates | length) == 1' "Software Team is the only built-in template"
assert_json_expr '.templates[0].id == "software-team"' "Template id is software-team"
assert_json_expr '(.templates[0].slots | length) == 6' "Software Team exposes six role slots"
assert_json_expr '([.templates[0].slots[] | select(.facilitator)] | length) == 1' "Exactly one facilitator slot"
assert_json_expr '[.templates[0].slots[] | select(.facilitator)][0].id == "scrum-master"' "The scrum master is the facilitator slot"
assert_json_expr '[.templates[0].slots[] | select(.id == "pm")][0].specialization | length > 0' "Slots carry role specializations"

# 18.4 Materialization validation: unknown template, missing bindings.
api_req "POST" "${TEAMS_CHANNEL_BASE}/templates/ghost/materialize" "${CHARLIE_TOKEN}" '{"name":"X","slug":"x"}'
assert_status "404" "Unknown template returns 404"

api_req "POST" "${TEAMS_CHANNEL_BASE}/templates/software-team/materialize" "${CHARLIE_TOKEN}" '{"name":"Half Team","slug":"half-team","slots":{"pm":{"spawn":true}}}'
assert_status "422" "Materializing with unbound slots returns 422"
assert_json_expr '[.error.details[]? | select(.field == "slots")] | length > 0' "The validation error lists the missing slot ids"

# 18.5 Materialize the Software Team, spawning all six agents.
TEAMS_SLOTS='{"pm":{"spawn":true},"architect":{"spawn":true},"scrum-master":{"spawn":true},"frontend":{"spawn":true},"backend":{"spawn":true},"tester":{"spawn":true}}'
api_req "POST" "${TEAMS_CHANNEL_BASE}/templates/software-team/materialize" "${CHARLIE_TOKEN}" "{\"name\":\"Dark Mode Build\",\"slug\":\"dark-mode\",\"purpose\":\"Ship dark mode.\",\"slots\":${TEAMS_SLOTS}}"
assert_status "201" "Materializing Software Team with all six spawns returns 201"
assert_json_expr '.channel.slug == "dark-mode"' "Materialized channel carries the requested slug"
assert_json_expr '.channel.conventions | contains("PLAN.md")' "Channel conventions carry the /project + PLAN.md prefill"
assert_json_expr '(.members | length) == 7' "Roster has the six agents plus the human creator"
assert_json_expr '([.members[] | select(.member_type == "agent")] | length) == 6' "Six agent memberships carry their slots"
assert_json_expr '([.members[] | select(.role == "facilitator")] | length) == 1' "Exactly one facilitator member on the roster"
assert_json_expr '[.members[] | select(.role == "facilitator")][0].handle == "scrum-master"' "The scrum master landed as the facilitator"
assert_json_expr '([.members[] | select(.member_type == "agent" and (.specialization | length > 0))] | length) == 6' "Every agent member carries its specialization"
assert_json_expr '(.created_agents | length) == 6' "Six agents were spawned"
TEAM_CHANNEL_ID=$(json_get '.channel.id')

# Slug conflicts are 409.
api_req "POST" "${TEAMS_CHANNEL_BASE}/templates/software-team/materialize" "${CHARLIE_TOKEN}" "{\"name\":\"Duplicate\",\"slug\":\"dark-mode\",\"slots\":${TEAMS_SLOTS}}"
assert_status "409" "Materializing onto a taken channel slug returns 409"

# The spawned agents are real workspace agents (spot-check one).
api_req "GET" "/api/v1/workspaces/${TEAMS_TENANT_SLUG}/agents/pm" "${CHARLIE_TOKEN}"
assert_status "200" "The spawned pm agent exists as a workspace agent"

# 18.6 Kickoff: the flagged post mints the work session (human-gated, D1).
KICKOFF_BASE="${TEAMS_CHANNEL_BASE}/${TEAM_CHANNEL_ID}"
api_req "POST" "${KICKOFF_BASE}/messages" "${CHARLIE_TOKEN}" '{"body":"ONCLAW_TEAMS_SMOKE kickoff: add dark mode to the settings page","is_kickoff":true}'
assert_status "201" "The kickoff post mints the work session"
assert_json_expr '.session.status == "open"' "The session opens"
assert_json_expr '.session.budget == 12' "The session carries the default 12-hop budget"
TEAM_SESSION_ID=$(json_get '.session.id')

api_req "POST" "${KICKOFF_BASE}/messages" "${CHARLIE_TOKEN}" '{"body":"ONCLAW_TEAMS_SMOKE a second kickoff attempt","is_kickoff":true}'
assert_status "409" "A second kickoff while the session is live returns 409"

# The awaiting state rides the channel view while the session is live.
api_req "GET" "${KICKOFF_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner gets the channel with its awaiting state"
assert_json_expr ".channel.active_session.id == \"${TEAM_SESSION_ID}\"" "The channel view carries the active session"

# 18.7 Hop accounting + in-session chain: the facilitator plans (hop 1) and
# hands off @pm → @architect → @backend → @tester. Depth 4 exceeds the v1
# chain cap, proving session bounds suspend it (D3); the tester tags the
# human, which pauses the session (D4). Poll until the pause lands.
TEAM_HOPS=0
for i in {1..40}; do
    api_req "GET" "${KICKOFF_BASE}/sessions/${TEAM_SESSION_ID}" "${CHARLIE_TOKEN}"
    TEAM_HOPS=$(json_get '.session.hops_used')
    if [[ "${TEAM_HOPS}" != "null" && "${TEAM_HOPS}" -ge 1 ]]; then
        break
    fi
    sleep 0.5
done
if [[ "${TEAM_HOPS}" -ge 1 ]]; then
    log_pass "Facilitator summon consumed hop 1 (hops_used: ${TEAM_HOPS})"
else
    log_fail "Session hops never advanced past 0 (last body: ${HTTP_BODY})"
fi

TEAM_PAUSED=0
for i in {1..80}; do
    api_req "GET" "${KICKOFF_BASE}/sessions/${TEAM_SESSION_ID}" "${CHARLIE_TOKEN}"
    if [[ "$(json_get '.session.status')" == "paused" && "$(json_get '.session.pause_reason')" == "awaiting-human" ]]; then
        TEAM_PAUSED=1
        break
    fi
    sleep 0.5
done
if [[ "${TEAM_PAUSED}" -ne 1 ]]; then
    log_fail "Session never paused awaiting-human (status: $(json_get '.session.status'), hops: $(json_get '.session.hops_used'))"
fi
log_pass "Agent-tagged-human paused the session as awaiting-human"
assert_json_expr '.session.hops_used >= 5' "Hops accounting shows the depth-4 in-session chain (caps suspended)"

# 18.8 Session reads: list (newest first) + detail payload.
api_req "GET" "${KICKOFF_BASE}/sessions" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the channel's work sessions"
assert_json_expr '(.sessions | length) == 1' "The list shows the one session"
assert_json_expr '.sessions[0].id == "'"${TEAM_SESSION_ID}"'"' "The list carries the minted session"

# 18.9 The human reply resumes the session (D4).
api_req "POST" "${KICKOFF_BASE}/messages" "${CHARLIE_TOKEN}" '{"body":"approved — proceed with the build"}'
assert_status "201" "The human member posts to the paused channel"
api_req "GET" "${KICKOFF_BASE}/sessions/${TEAM_SESSION_ID}" "${CHARLIE_TOKEN}"
assert_json_expr '.session.status == "open"' "The human post resumed the session (open)"

# 18.10 Facilitator close: ask the scrum master; the ONCLAW_TEAMS_CLOSE marker
# steers the mock to a session.close tool call, and the session closes with a
# stored summary (D2 — only the facilitator closes).
api_req "POST" "${KICKOFF_BASE}/messages" "${CHARLIE_TOKEN}" '{"body":"@scrum-master ONCLAW_TEAMS_CLOSE wrap up and close the session"}'
assert_status "201" "The human asks the facilitator to close"

TEAM_CLOSED=0
for i in {1..60}; do
    api_req "GET" "${KICKOFF_BASE}/sessions/${TEAM_SESSION_ID}" "${CHARLIE_TOKEN}"
    if [[ "$(json_get '.session.status')" == "closed" ]]; then
        TEAM_CLOSED=1
        break
    fi
    sleep 0.5
done
if [[ "${TEAM_CLOSED}" -ne 1 ]]; then
    log_fail "Facilitator never closed the session (status: $(json_get '.session.status'), body: ${HTTP_BODY})"
fi
log_pass "The facilitator closed the session via session.close"
assert_json_expr '.session.summary | contains("Dark mode")' "The close summary is stored on the session"
assert_json_expr '.session.closed_at != null' "The session carries its close timestamp"

# -----------------------------------------------------------------------------
# 19. /v1 Compact Command (chat-compact-command)
# -----------------------------------------------------------------------------
log_step "19. /v1 Compact Command"

# 19.1 Bind a session with two ordinary turns. The ONCLAW_V1_SMOKE marker in
# the transcript steers the mock provider's live branch — which also serves
# the summarizer's own completion call during the compact turn below (the
# transcript the summarizer forwards carries the marker).
COMPACT_SESSION="sess-compact-$(date +%s)-${RANDOM}"
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE compact history turn one","stream":true,"metadata":{"onclaw_session":"'"${COMPACT_SESSION}"'"}}'
assert_status "200" "Compact-session ordinary turn one accepted"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.completed"' || log_fail "Compact-session turn one missing response.completed"

api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE compact history turn two","stream":true,"metadata":{"onclaw_session":"'"${COMPACT_SESSION}"'"}}'
assert_status "200" "Compact-session ordinary turn two accepted"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.completed"' || log_fail "Compact-session turn two missing response.completed"

# 19.2 Compact turn: metadata.onclaw_command routes the turn to the
# summarizer with the focus text in input; the SSE body carries the
# onclaw:context_compacted frame with the token estimates and terminates with
# a completed response carrying the summarizer's usage.
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE keep the deployment runbook details","stream":true,"metadata":{"onclaw_session":"'"${COMPACT_SESSION}"'","onclaw_command":"compact"}}'
assert_status "200" "Compact turn accepted on the bound session"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"onclaw:context_compacted"' || log_fail "Compact stream missing onclaw:context_compacted frame"
printf '%s' "${HTTP_BODY}" | grep -q '"tokens_before":' || log_fail "context_compacted frame missing tokens_before"
printf '%s' "${HTTP_BODY}" | grep -q '"tokens_after":' || log_fail "context_compacted frame missing tokens_after"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.completed"' || log_fail "Compact stream missing response.completed"
printf '%s' "${HTTP_BODY}" | grep -q '"usage":{' || log_fail "Compact completed response carries no usage block"
printf '%s' "${HTTP_BODY}" | grep -q '^data: \[DONE\]' || log_fail "Compact stream missing [DONE] sentinel"
log_pass "Compact turn streamed context_compacted (tokens) and completed with usage"

# 19.3 Compact never births (design D2): an unknown metadata.onclaw_session
# fails bind-only not-found instead of minting an empty session.
api_req "POST" "/v1/responses" "${V1_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE ghost compact","metadata":{"onclaw_session":"sess-compact-ghost-unknown","onclaw_command":"compact"}}'
assert_status "404" "Compact against an unknown session returns 404 (bind-only, never births)"

# -----------------------------------------------------------------------------
# 20. Durable Agent Session Index (agent-session-index)
# -----------------------------------------------------------------------------
log_step "20. Durable Agent Session Index"

# 20.1 The index write rides the runner's persistent-run start, so one /v1
# turn on a bound session births the row. The turn runs as Charlie (the
# workspace Owner) so this section can exercise the agents.write delete; the
# chat key machinery is member-agnostic.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/api-keys/exchange" "${CHARLIE_TOKEN}"
assert_status "201" "Owner exchanges a workspace-scoped chat key for the index section"
V1C_KEY=$(json_get '.key')

IDX_SESSION="sess-index-$(date +%s)-${RANDOM}"
api_req "POST" "/v1/responses" "${V1C_KEY}" '{"model":"test-agent","input":"ONCLAW_V1_SMOKE fix the login flow bug","stream":true,"metadata":{"onclaw_session":"'"${IDX_SESSION}"'"}}'
assert_status "200" "Index-session birth turn accepted"
printf '%s' "${HTTP_BODY}" | grep -q '"type":"response.completed"' || log_fail "Index-session stream missing response.completed"

# 20.2 The per-user listing shows the session with its birth title (the first
# input line, trimmed — short enough here that no truncation applies), the
# running flag settled to false (the turn already completed), and exactly the
# documented row keys.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the agent's session index"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")] | length == 1" "Listing shows the turn's session"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")][0].title == \"ONCLAW_V1_SMOKE fix the login flow bug\"" "Session row carries the input-derived title"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")][0].title | length > 0" "Session title is non-empty"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")][0].running == false" "Completed turn reports running false"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")][0] | keys == [\"created_at\",\"id\",\"last_active_at\",\"running\",\"session_id\",\"title\"]" "Session row carries exactly the documented keys"

# 20.3 The listing is private per user: Dave's own sessions never include
# Charlie's row.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions" "${DAVE_TOKEN}"
assert_status "200" "Member lists their own session index"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")] | length == 0" "Charlie's session is absent from Dave's listing"

# 20.4 A foreign delete with agents.write is a 404 — a foreign-owned row is
# indistinguishable from an absent one (no existence leak). Bob is Admin.
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions/${IDX_SESSION}" "${BOB_TOKEN}"
assert_status "404" "Foreign user's delete of a session they do not own is 404"

# 20.5 The owner's delete is a 204 soft delete; the row disappears from
# subsequent listings while the transcript stays on disk.
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions/${IDX_SESSION}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes their own session (204)"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions" "${CHARLIE_TOKEN}"
assert_status "200" "Owner re-lists after the delete"
assert_json_expr "[.sessions[] | select(.session_id == \"${IDX_SESSION}\")] | length == 0" "Deleted session no longer appears in the listing"

# The transcript itself survives the soft delete (direct-id access unchanged).
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions/${IDX_SESSION}/events?limit=100" "${CHARLIE_TOKEN}"
assert_status "200" "Soft-deleted session's transcript remains retrievable by direct id"
assert_json_expr '(.events | length) >= 2' "Soft-deleted session's events still carry the birth turn"

# -----------------------------------------------------------------------------
# 21. Chat Attachments (workspace-attachments, local driver)
# -----------------------------------------------------------------------------
log_step "21. Chat Attachments & Workspace Storage Config"

# NOTE: turn-reference coverage (the /v1 input parts carrying capability-URL
# attachments) belongs to the /v1 multimodal-input task group and is added
# when that group lands; this section covers the out-of-band upload surface.

# 21.1 Generate a small valid PNG and upload it as a workspace attachment.
ATT_PNG="${TMP_DIR}/att_shot.png"
echo "iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAFUlEQVR42mNkYPjPwMDAwMgABIkAKQ0DFXsMkFcAAAAASUVORK5CYII=" | base64 -d > "${ATT_PNG}"

api_upload "/api/v1/workspaces/${TENANT_SLUG}/attachments" "${ALICE_TOKEN}" "${ATT_PNG}" "file"
assert_status "201" "Owner uploads a small PNG attachment (201)"
assert_json_expr '.id != null and .id != ""' "Upload response carries the attachment id"
assert_json_expr '.name == "att_shot.png"' "Upload response carries the original filename"
assert_json_expr '.mime == "image/png"' "Upload response carries the server-sniffed mime"
ATT_SIZE=$(stat -f%z "${ATT_PNG}" 2>/dev/null || stat -c%s "${ATT_PNG}")
assert_json_expr ".size == ${ATT_SIZE}" "Upload response carries the byte size"
ATT_URL=$(json_get '.url')
if [[ "${ATT_URL}" =~ ^/api/v1/files/[0-9a-f]{32}$ ]]; then
    log_pass "Capability URL is the proxied files path over a 32-hex key (${ATT_URL})"
else
    log_fail "Capability URL malformed: ${ATT_URL}"
fi

# 21.2 The capability URL serves the stored bytes without authentication,
# byte-for-byte.
api_req "GET" "${ATT_URL}" ""
assert_status "200" "Capability URL serves the attachment without authentication (200)"
if cmp -s "${TMP_DIR}/body.tmp" "${ATT_PNG}"; then
    log_pass "Served bytes are identical to the uploaded file"
else
    log_fail "Served bytes differ from the uploaded attachment"
fi

# 21.3 Oversize image: PNG header over the 5 MB cap → 413.
printf '\x89PNG\x0D\x0A\x1A\x0A' > "${TMP_DIR}/att_huge.png"
head -c 5500000 /dev/zero >> "${TMP_DIR}/att_huge.png"
api_upload "/api/v1/workspaces/${TENANT_SLUG}/attachments" "${ALICE_TOKEN}" "${TMP_DIR}/att_huge.png" "file"
assert_status "413" "Oversize image upload rejected 413"
assert_json_expr '.error.message | contains("5242880")' "413 message names the 5 MB image cap"

# 21.4 Drop-lane document upload (report.docx) & legacy office rejection (legacy.doc).
# A real OOXML package (zip magic), built inline — the server sniffs magic
# bytes, never the extension, so a text body named .docx would be refused as
# a text/content mismatch. Lane classification itself is unit-covered; the
# wire carries the sniffed mime, not the lane.
python3 - "${TMP_DIR}/report.docx" <<'PYEOF'
import sys
import zipfile
with zipfile.ZipFile(sys.argv[1], "w") as z:
    z.writestr("[Content_Types].xml",
               '<?xml version="1.0" encoding="UTF-8"?>'
               '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>')
    z.writestr("word/document.xml", "<doc/>")
PYEOF
api_upload "/api/v1/workspaces/${TENANT_SLUG}/attachments" "${ALICE_TOKEN}" "${TMP_DIR}/report.docx" "file"
assert_status "201" "Modern office format (docx) upload accepted (drop-lane document, 201)"
assert_json_expr '.mime == "application/zip"' "docx mime is sniffed from OOXML magic bytes, not the extension"

echo "legacy binary doc content" > "${TMP_DIR}/legacy.doc"
api_upload "/api/v1/workspaces/${TENANT_SLUG}/attachments" "${ALICE_TOKEN}" "${TMP_DIR}/legacy.doc" "file"
assert_status "400" "Legacy office-format (.doc) upload rejected 400"
assert_json_expr '.error.message | ascii_downcase | contains("legacy binary office formats")' "Legacy office rejection suggests converting to modern formats or PDF"

# 21.5 Storage configuration: unconfigured workspace reads as the local
# default, masked shape (no s3 fields, no secret material).
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/storage" "${ALICE_TOKEN}"
assert_status "200" "Owner reads the storage configuration"
assert_json_expr '.driver == "local"' "Unconfigured workspace reports the local driver"
assert_json_expr 'has("endpoint") | not' "Local default omits the s3 fields"

# 21.6 Owner saves the local driver choice (probe-free) → 200.
api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/storage" "${ALICE_TOKEN}" '{"driver":"local"}'
assert_status "200" "Owner saves the local storage configuration"
assert_json_expr '.driver == "local"' "Saved configuration reports local"

# 21.7 Settings management is Owner/Admin: the Member token is 403 on both
# reads (masked) and writes.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/storage" "${DAVE_TOKEN}"
assert_status "403" "Member cannot read the storage configuration (403)"
api_req "PUT" "/api/v1/workspaces/${TENANT_SLUG}/storage" "${DAVE_TOKEN}" '{"driver":"local"}'
assert_status "403" "Member cannot change the storage configuration (403)"

# -----------------------------------------------------------------------------
# 22. Schedulers: Permission Guards, Run-Now, Run History, Channel Delivery &
#     Pause Flow (integrate-scheduler)
# -----------------------------------------------------------------------------
log_step "22. Schedulers: Run-Now, History, Channel Delivery & Pause"

SCHED_BASE="/api/v1/workspaces/${TENANT_SLUG}/schedulers"
# The recurring expression fires Jan 1 03:30 in the workspace timezone — a
# year out, so the ticker's claim loop can never fire it mid-test.
SCHED_EXPR="30 3 1 1 *"
# The ONCLAW_V1_SMOKE marker steers the section-13 mock provider into its
# plain-assistant-reply branch, so the run-now agent run genuinely completes.
SCHED_PROMPT="ONCLAW_V1_SMOKE scheduler smoke: summarize the workspace state"

# 22.1 Permission guards (D10): reads ride scheduler.read (granted to every
# built-in role), writes ride scheduler.write (Owner/Admin only).
api_req "GET" "${SCHED_BASE}" "${CLI_USER_TOKEN}"
assert_status "200" "Member can list schedulers (scheduler.read)"
assert_json_expr '(.schedulers | length) == 0' "Scheduler list starts empty"

api_req "POST" "${SCHED_BASE}" "${CLI_USER_TOKEN}" "{\"name\":\"Nope\",\"agent_id\":\"${AGENT_ID}\",\"prompt\":\"x\",\"kind\":\"recurring\",\"expr\":\"${SCHED_EXPR}\"}"
assert_status "403" "Member cannot create schedulers (scheduler.write 403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

# 22.2 Owner creates a recurring scheduler bound to the smoke agent; the next
# fire time is store-derived, never carried from input (D12).
api_req "POST" "${SCHED_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Morning Digest\",\"agent_id\":\"${AGENT_ID}\",\"prompt\":\"${SCHED_PROMPT}\",\"kind\":\"recurring\",\"expr\":\"${SCHED_EXPR}\",\"delivery\":{\"type\":\"thread\"}}"
assert_status "201" "Owner creates a recurring scheduler bound to the smoke agent"
assert_json_expr ".scheduler.kind == \"recurring\" and .scheduler.expr == \"${SCHED_EXPR}\"" "Create echoes kind and the raw expression"
assert_json_expr ".scheduler.agent_id == \"${AGENT_ID}\"" "Scheduler carries the bound agent id"
assert_json_expr '.scheduler.enabled == true' "New scheduler starts enabled"
assert_json_expr '.scheduler.next_run_at != null' "Enabled scheduler carries a derived next fire time"
assert_json_expr '.scheduler.human_label | length > 0' "Read view derives the human label"
SCHED_ID=$(json_get '.scheduler.id')

# A garbage cron is a fielded 422 from the shared validator (D11).
api_req "POST" "${SCHED_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Broken Cron\",\"agent_id\":\"${AGENT_ID}\",\"prompt\":\"x\",\"kind\":\"recurring\",\"expr\":\"not a cron\"}"
assert_status "422" "Invalid cron expression is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "expr")] | length > 0' "Validation error fields expr"

# 22.3 Run-now (D9): direct dispatch recording trigger manual. The row comes
# back live and drains asynchronously; poll the run history to terminal. The
# ONCLAW_V1_SMOKE prompt drives a real agent run against the mock provider
# (the same wiring sections 14/17 use).
api_req "POST" "${SCHED_BASE}/${SCHED_ID}/run" "${CHARLIE_TOKEN}"
assert_status "200" "Owner runs the scheduler now"
assert_json_expr '.run.trigger == "manual"' "Run-now records trigger manual"
assert_json_expr '.run.status == "running"' "Run-now returns the live run row"
assert_json_expr ".run.session_id | startswith(\"sched_${SCHED_ID}_\")" "Run session id has the sched_<schedulerID>_<ts> shape"
# Tracing stays invisible when disabled (integrate-langfuse-tracing 5.2): the
# run payloads always carry langfuse_url, and it is null on this unconfigured
# instance — present-and-null, not absent.
assert_json_expr '.run.langfuse_url == null and (.run | has("langfuse_url"))' "Run-now payload exposes langfuse_url null without tracing"

SCHED_RUNS_TERMINAL=0
for i in {1..60}; do
    api_req "GET" "${SCHED_BASE}/${SCHED_ID}/runs?limit=100" "${CHARLIE_TOKEN}"
    SCHED_RUN_STATUS=$(json_get '.runs[0].status')
    if [[ "${SCHED_RUN_STATUS}" != "running" && "${SCHED_RUN_STATUS}" != "null" && "${SCHED_RUN_STATUS}" != "" ]]; then
        SCHED_RUNS_TERMINAL=1
        break
    fi
    sleep 0.5
done
if [[ "${SCHED_RUNS_TERMINAL}" -ne 1 ]]; then
    log_fail "Scheduler run never reached a terminal status (last: ${SCHED_RUN_STATUS})"
fi
assert_status "200" "Scheduler runs endpoint serves the history"
assert_json_expr '.runs[0].status == "completed"' "Scheduler run completed (mock provider produced a real agent run)"
assert_json_expr '.runs[0].trigger == "manual"' "History row records the manual trigger"
assert_json_expr '.runs[0].tokens_used > 0' "Completed run metered token usage"
assert_json_expr '.runs[0].langfuse_url == null and (.runs[0] | has("langfuse_url"))' "Run history rows expose langfuse_url null without tracing"
assert_json_expr '.total >= 1' "Runs history reports its total"
SCHED_RUN_SESSION=$(json_get '.runs[0].session_id')

# 22.4 The run transcript is a real session artifact (D7): fetchable through
# the standard agent session-events read by its sched_ session id.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/test-agent/sessions/${SCHED_RUN_SESSION}/events?limit=100" "${CHARLIE_TOKEN}"
assert_status "200" "Scheduler run transcript resolves by its session id"
assert_json_expr '(.events | length) >= 2' "Run transcript carries the user prompt and the assistant reply"
assert_json_expr '[.events[]?.message.content // ""] | join(" ") | contains("'"${SCHED_PROMPT}"'")' "Run transcript contains the scheduler prompt"
assert_json_expr '[.events[]?.message.content // ""] | join(" ") | contains("live reply")' "Run transcript contains the mock provider's final reply"

# 22.5 Channel delivery (D8): a scheduler targeting a channel posts the run's
# final reply into the feed through the chokepoint as an agent author.
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Scheduler Digest","slug":"sched-digest","purpose":"Scheduler delivery target."}'
assert_status "201" "Owner creates the scheduler delivery channel"
SCHED_CHANNEL_ID=$(json_get '.channel.id')

api_req "POST" "${CHANNELS_BASE}/${SCHED_CHANNEL_ID}/members" "${CHARLIE_TOKEN}" "{\"member_type\":\"agent\",\"agent_id\":\"${AGENT_ID}\",\"specialization\":\"Scheduled digests\"}"
assert_status "201" "Owner adds the scheduler's agent to the delivery channel"

api_req "POST" "${SCHED_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Channel Digest\",\"agent_id\":\"${AGENT_ID}\",\"prompt\":\"${SCHED_PROMPT}\",\"kind\":\"recurring\",\"expr\":\"${SCHED_EXPR}\",\"delivery\":{\"type\":\"channel\",\"channel_id\":\"${SCHED_CHANNEL_ID}\"}}"
assert_status "201" "Owner creates a channel-delivery scheduler"
assert_json_expr ".scheduler.delivery.type == \"channel\" and .scheduler.delivery.channel_id == \"${SCHED_CHANNEL_ID}\"" "Create echoes the channel delivery target"
SCHED_CHAN_SCHED_ID=$(json_get '.scheduler.id')

api_req "POST" "${SCHED_BASE}" "${CHARLIE_TOKEN}" "{\"name\":\"Channel Digest\",\"agent_id\":\"${AGENT_ID}\",\"prompt\":\"dup\",\"kind\":\"recurring\",\"expr\":\"${SCHED_EXPR}\"}"
assert_status "409" "Duplicate scheduler name per agent returns 409"

api_req "POST" "${SCHED_BASE}/${SCHED_CHAN_SCHED_ID}/run" "${CHARLIE_TOKEN}"
assert_status "200" "Owner runs the channel-delivery scheduler now"

SCHED_RUNS_TERMINAL=0
for i in {1..60}; do
    api_req "GET" "${SCHED_BASE}/${SCHED_CHAN_SCHED_ID}/runs?limit=100" "${CHARLIE_TOKEN}"
    SCHED_RUN_STATUS=$(json_get '.runs[0].status')
    if [[ "${SCHED_RUN_STATUS}" != "running" && "${SCHED_RUN_STATUS}" != "null" && "${SCHED_RUN_STATUS}" != "" ]]; then
        SCHED_RUNS_TERMINAL=1
        break
    fi
    sleep 0.5
done
if [[ "${SCHED_RUNS_TERMINAL}" -ne 1 ]]; then
    log_fail "Channel-delivery run never reached a terminal status (last: ${SCHED_RUN_STATUS})"
fi
assert_json_expr '.runs[0].status == "completed"' "Channel-delivery run completed"
assert_json_expr '.runs[0].delivery_status == "delivered"' "Run records the delivered channel delivery"

# The final reply lands in the channel feed as an agent-authored message.
SCHED_CHAN_MSG=0
for i in {1..20}; do
    api_req "GET" "${CHANNELS_BASE}/${SCHED_CHANNEL_ID}/messages" "${CHARLIE_TOKEN}"
    if [[ "$(json_get '([.messages[] | select(.author_type == "agent")] | length)')" -ge 1 ]]; then
        SCHED_CHAN_MSG=1
        break
    fi
    sleep 0.5
done
if [[ "${SCHED_CHAN_MSG}" -ne 1 ]]; then
    log_fail "Channel delivery never posted the run's final reply into the feed"
fi
log_pass "Channel delivery posted the run's final reply into the feed"
assert_json_expr '[.messages[] | select(.author_type == "agent")][0].body | contains("live reply")' "Delivered reply body comes from the mock provider"

# 22.6 Workspace-wide run feed: both schedulers' runs, enriched rows.
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/scheduler-runs?limit=100" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the workspace-wide scheduler runs"
assert_json_expr '.total >= 2' "Workspace-wide feed counts both schedulers' runs"
assert_json_expr '.runs[0].scheduler_name | length > 0' "Feed rows are enriched with the scheduler name"
assert_json_expr '.runs[0].langfuse_url == null and (.runs[0] | has("langfuse_url"))' "Workspace-wide feed rows expose langfuse_url null without tracing"

# 22.7 Pause flow (D9): disabling clears the derived next fire time; run-now
# still executes a paused scheduler and never re-enables it.
api_req "PATCH" "${SCHED_BASE}/${SCHED_ID}" "${CHARLIE_TOKEN}" '{"enabled":false}'
assert_status "200" "Owner disables the scheduler"
assert_json_expr '.scheduler.enabled == false' "Patch disables the scheduler"
assert_json_expr '.scheduler.next_run_at == null' "Pausing clears the next fire time"

api_req "GET" "${SCHED_BASE}/${SCHED_ID}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads the paused scheduler"
assert_json_expr '.scheduler.next_run_at == null' "Paused scheduler reads without a next fire time"

api_req "POST" "${SCHED_BASE}/${SCHED_ID}/run" "${CHARLIE_TOKEN}"
assert_status "200" "Run-now still executes a paused scheduler"
assert_json_expr '.run.trigger == "manual"' "Paused run-now records trigger manual"

SCHED_RUNS_TERMINAL=0
for i in {1..60}; do
    api_req "GET" "${SCHED_BASE}/${SCHED_ID}/runs?limit=100" "${CHARLIE_TOKEN}"
    SCHED_RUN_STATUS=$(json_get '.runs[0].status')
    if [[ "${SCHED_RUN_STATUS}" != "running" && "${SCHED_RUN_STATUS}" != "null" && "${SCHED_RUN_STATUS}" != "" ]]; then
        SCHED_RUNS_TERMINAL=1
        break
    fi
    sleep 0.5
done
if [[ "${SCHED_RUNS_TERMINAL}" -ne 1 ]]; then
    log_fail "Paused scheduler's run-now never reached a terminal status (last: ${SCHED_RUN_STATUS})"
fi
assert_json_expr '.runs[0].status == "completed"' "Paused scheduler's run-now completed"

api_req "GET" "${SCHED_BASE}/${SCHED_ID}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner re-reads the scheduler after the paused run-now"
assert_json_expr '.scheduler.enabled == false' "Run-now did not re-enable the paused scheduler"
assert_json_expr '.scheduler.next_run_at == null' "Paused scheduler still has no next fire time"

# -----------------------------------------------------------------------------
# 23. Telegram Gateway: Multi-Bot Accounts, Write-Only Token, Bindings, Pairing &
#     Webhook Secret Enforcement (multi-bot-gateways)
# -----------------------------------------------------------------------------
log_step "23. Telegram Gateway: Multi-Bot Config, Bindings, Pairing & Webhook"

GW_BASE="/api/v1/workspaces/${TENANT_SLUG}/gateways/telegram"
GW_TOKEN_1="123456:AAH-smoke-bot-token-9876"
GW_TOKEN_2="654321:BBH-smoke-bot-token-5432"

# Create a second agent in the workspace to test multi-bot binding
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" "{\"name\":\"Beacon\",\"slug\":\"beacon\",\"role\":\"Assistant\",\"description\":\"Second agent\",\"brief\":\"Second agent\",\"model\":\"gpt-4\",\"provider_id\":\"${MOCK_PROV_ID}\"}"
assert_status "201" "Owner creates a second agent (Beacon) for multi-bot tests"
AGENT_2_ID=$(json_get '.agent.id')

# 23.1 Auth gates: the ingress requires a session; a plain Member holds no
# gateways.write, so config reads AND writes are 403 (pairing stays
# member-level — asserted in 23.6).
api_req "GET" "${GW_BASE}" ""
assert_status "401" "Unauthenticated gateway config read returns 401"

api_req "GET" "${GW_BASE}" "${DAVE_TOKEN}"
assert_status "403" "Member cannot read the gateway config (gateways.write 403)"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

api_req "POST" "${GW_BASE}" "${DAVE_TOKEN}" "{\"token\":\"${GW_TOKEN_1}\",\"agent_id\":\"${AGENT_ID}\"}"
assert_status "403" "Member cannot write the gateway config (403)"

# 23.2 Initially no accounts are configured: GET returns empty list []
api_req "GET" "${GW_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists gateway accounts (initially empty)"
assert_json_expr '(. | length) == 0' "No accounts configured initially"

# 23.2a Validation: missing agent_id or token is rejected (422)
api_req "POST" "${GW_BASE}" "${CHARLIE_TOKEN}" "{\"token\":\"${GW_TOKEN_1}\"}"
assert_status "422" "Connecting bot without agent_id is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "agent_id")] | length > 0' "Validation error fields agent_id"

# 23.3 Create first bot account (bound to AGENT_ID)
api_req "POST" "${GW_BASE}" "${CHARLIE_TOKEN}" "{\"token\":\"${GW_TOKEN_1}\",\"agent_id\":\"${AGENT_ID}\",\"transport\":\"long_polling\"}"
assert_status "201" "Owner creates first gateway account bound to agent 1"
assert_json_expr '.id != null' "Create POST returns the gateway row id"
assert_json_expr '.platform == "telegram"' "Gateway platform is telegram"
assert_json_expr '.enabled == true' "A fresh gateway starts enabled"
assert_json_expr '.token_hint == "9876"' "Config carries only the last-4 token hint"
assert_json_expr ".agent_id == \"${AGENT_ID}\"" "Gateway carries the bound agent id"
GW1_ID=$(json_get '.id')

if echo "${HTTP_BODY}" | grep -q "${GW_TOKEN_1}"; then
    log_fail "Config response echoed the plaintext bot token!"
else
    log_pass "Config response never echoes the plaintext bot token"
fi
if echo "${HTTP_BODY}" | grep -q '"v1:'; then
    log_fail "Config response leaked the token ciphertext envelope!"
else
    log_pass "Config response never leaks the ciphertext envelope"
fi

# 23.4 Create second bot account (bound to AGENT_2_ID)
api_req "POST" "${GW_BASE}" "${CHARLIE_TOKEN}" "{\"token\":\"${GW_TOKEN_2}\",\"agent_id\":\"${AGENT_2_ID}\",\"transport\":\"long_polling\"}"
assert_status "201" "Owner creates second gateway account bound to agent 2"
assert_json_expr '.id != null' "Second gateway row id returned"
assert_json_expr ".agent_id == \"${AGENT_2_ID}\"" "Second gateway bound to agent 2"
assert_json_expr '.token_hint == "5432"' "Second gateway carries hint 5432"
GW2_ID=$(json_get '.id')

# 23.4b List accounts: both bot accounts appear in the workspace list
api_req "GET" "${GW_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists all gateway accounts"
assert_json_expr '(. | length) == 2' "Both gateway accounts are listed"

# 23.4c Get specific account
api_req "GET" "${GW_BASE}/${GW1_ID}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads account 1"
assert_json_expr '.token_hint == "9876"' "GET carries the last-4 hint"

# 23.5 Account update (change bound agent or rotate token)
api_req "PUT" "${GW_BASE}/${GW1_ID}" "${CHARLIE_TOKEN}" '{"transport":"webhook","webhook_url":"https://example.com/smoke-webhook"}'
assert_status "200" "Owner updates account 1 transport to webhook"
assert_json_expr '.transport == "webhook"' "Account 1 transport is webhook"
assert_json_expr '.webhook_url == "https://example.com/smoke-webhook"' "Webhook URL stored"

api_req "PUT" "${GW_BASE}/${GW1_ID}" "${CHARLIE_TOKEN}" '{"transport":"long_polling"}'
assert_status "200" "Owner updates account 1 transport back to long_polling"
assert_json_expr '.transport == "long_polling"' "Account 1 transport is long_polling"

# 23.6 Pairing is member-level: a plain Member can mint and revoke tokens for
# themselves while still unable to touch the config.
api_req "POST" "${GW_BASE}/pairing-tokens" "${DAVE_TOKEN}" '{}'
assert_status "201" "Member mints a pairing token (member-gated)"
assert_json_expr '(.token.token | length) >= 32' "Pairing token carries its full crypto-random value"
assert_json_expr '.token.expires_at != null' "Pairing token carries an expiry"
PAIR_TOKEN=$(json_get '.token.token')

api_req "GET" "${GW_BASE}/links/me" "${DAVE_TOKEN}"
assert_status "200" "Member reads their (absent) Telegram link"
assert_json_expr '.link == null' "Unpaired member reads a null link"

api_req "DELETE" "${GW_BASE}/links/me" "${DAVE_TOKEN}"
assert_status "404" "Self unpair with no link is 404"

# Revoke lifecycle: an unconsumed token can be cancelled exactly once.
api_req "DELETE" "${GW_BASE}/pairing-tokens/${PAIR_TOKEN}" "${DAVE_TOKEN}"
assert_status "204" "Member revokes the minted pairing token (204)"
api_req "DELETE" "${GW_BASE}/pairing-tokens/${PAIR_TOKEN}" "${DAVE_TOKEN}"
assert_status "404" "Revoking an already-revoked token is 404"

# 23.7 Enable / disable lifecycle per account: enabling flips the flag on that account
api_req "POST" "${GW_BASE}/${GW1_ID}/enable" "${CHARLIE_TOKEN}" '{}'
assert_status "200" "Owner enables bot account 1"

api_req "GET" "${GW_BASE}/${GW1_ID}" "${CHARLIE_TOKEN}"
assert_json_expr '.enabled == true' "Enabled account 1 reports enabled true"

api_req "POST" "${GW_BASE}/${GW1_ID}/disable" "${CHARLIE_TOKEN}" '{}'
assert_status "200" "Owner disables bot account 1"

api_req "GET" "${GW_BASE}/${GW1_ID}" "${CHARLIE_TOKEN}"
assert_json_expr '.enabled == false' "Disabled account 1 reports enabled false"

api_req "POST" "${GW_BASE}/${GW1_ID}/enable" "${DAVE_TOKEN}" '{}'
assert_status "403" "Member cannot enable the gateway (403)"

# 23.8 Group bindings: create naming the owning gateway_id, duplicate conflict, fielded validation, list, delete.
SMOKE_CHAT_ID="-100${RANDOM}${RUN_ID: -4}"
api_req "POST" "${GW_BASE}/bindings" "${CHARLIE_TOKEN}" "{\"gateway_id\":\"${GW1_ID}\",\"agent_id\":\"${AGENT_ID}\",\"platform_chat_id\":\"${SMOKE_CHAT_ID}\",\"chat_title\":\"Smoke Ops\"}"
assert_status "201" "Owner binds a Telegram group naming gateway 1"
assert_json_expr ".agent_id == \"${AGENT_ID}\"" "Binding carries the bound agent"
assert_json_expr ".gateway_id == \"${GW1_ID}\"" "Binding carries owning gateway_id"
assert_json_expr '.platform == "telegram"' "Binding platform is telegram"
BINDING_ID=$(json_get '.id')

api_req "POST" "${GW_BASE}/bindings" "${CHARLIE_TOKEN}" "{\"gateway_id\":\"${GW1_ID}\",\"agent_id\":\"${AGENT_ID}\",\"platform_chat_id\":\"${SMOKE_CHAT_ID}\"}"
assert_status "409" "Binding the same chat twice is rejected (409)"
assert_json_expr '.error.code == "conflict"' "Binding conflict error code is conflict"

api_req "POST" "${GW_BASE}/bindings" "${CHARLIE_TOKEN}" '{}'
assert_status "422" "Binding without agent/chat ids is rejected (422)"

api_req "GET" "${GW_BASE}/bindings" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the gateway bindings"
assert_json_expr '(. | length) == 1' "Bindings list contains the created binding"

api_req "DELETE" "${GW_BASE}/bindings/${BINDING_ID}" "${DAVE_TOKEN}"
assert_status "403" "Member cannot delete bindings (403)"

api_req "DELETE" "${GW_BASE}/bindings/${BINDING_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes the binding (204)"

api_req "GET" "${GW_BASE}/bindings" "${CHARLIE_TOKEN}"
assert_json_expr '(. | length) == 0' "Deleted binding is gone from the list"

# 23.9 The public webhook ingress authenticates on the derived secret for that gateway account:
api_req "POST" "/api/v1/webhooks/telegram/${GW1_ID}" "" '{}'
assert_status "401" "Webhook POST without the secret token is 401"

api_req "POST" "/api/v1/webhooks/telegram/00000000-0000-0000-0000-000000000000" "" '{}'
assert_status "401" "Webhook POST for an unknown gateway still demands the secret (no existence leak)"

# Delete second bot account
api_req "DELETE" "${GW_BASE}/${GW2_ID}" "${CHARLIE_TOKEN}"
assert_status "204" "Owner deletes bot account 2 (204)"

api_req "GET" "${GW_BASE}/${GW2_ID}" "${CHARLIE_TOKEN}"
assert_status "404" "Deleted bot account 2 is gone (404)"

# -----------------------------------------------------------------------------
# 24. WhatsApp Gateway: Lane Validation, Write-Only Cloud Envelope, Webhook
#     Ingress, Stubbed Meta Delivery & Multi-Device Surface
#     (add-whatsapp-gateway tasks 9.1/9.3)
# -----------------------------------------------------------------------------
log_step "24. WhatsApp Gateway: Lane Config, Webhook & Stubbed Meta Delivery"

WA_BASE="/api/v1/workspaces/${TENANT_SLUG}/gateways/whatsapp"
WA_PHONE_ID="123456789012345"
WA_SENDER="15559998888"
WA_APP_SECRET="smoke-wa-app-secret"
WA_VERIFY_TOKEN="smoke-wa-verify-token"
WA_ACCESS_TOKEN="smoke-wa-access-token"

# 24.0 Stubbed Meta Graph API (design D8: ingestion is webhook-only; the
# adapter's sends and probes are retargeted here via
# ONCLAW_WHATSAPP_CLOUD_API_BASE, set on the server at boot). Every POST body
# is appended to a log file so the delivery assertions can poll it.
cat > "${TMP_DIR}/wa_stub.py" <<PY
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1])
LOG = sys.argv[2]
count = [0]

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        # Phone-number metadata probe (GET /{version}/{phone_number_id}).
        pid = self.path.rstrip("/").split("/")[-1]
        self._send(200, {"id": pid, "verified_name": "Smoke Cloud", "display_phone_number": "+1 555 000 1111", "quality_rating": "GREEN"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        with open(LOG, "a") as f:
            f.write(raw.decode("utf-8", "replace") + "\n")
        count[0] += 1
        self._send(200, {"messaging_product": "whatsapp", "contacts": [], "messages": [{"id": "wamid.smoke-out-%d" % count[0]}]})

HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
python3 "${TMP_DIR}/wa_stub.py" "${WA_STUB_PORT}" "${TMP_DIR}/wa_stub.log" &
WA_STUB_PID=$!
sleep 0.5
log_pass "Stubbed Meta Graph API listening on 127.0.0.1:${WA_STUB_PORT}"

# wa_sig computes the X-Hub-Signature-256 header for a raw body (HMAC-SHA256
# hex under the app secret, "sha256=" prefixed — design D8).
wa_sig() {
    printf 'sha256=%s' "$(printf '%s' "$1" | openssl dgst -sha256 -hmac "${WA_APP_SECRET}" | sed 's/^.* //')"
}

# 24.1 Auth gates mirror Telegram: unauthenticated reads are 401 and a plain
# Member holds no gateways.write on any admin route.
api_req "GET" "${WA_BASE}" ""
assert_status "401" "WhatsApp config read without a session returns 401"

api_req "GET" "${WA_BASE}" "${DAVE_TOKEN}"
assert_status "403" "Member cannot read the WhatsApp config (gateways.write 403)"

api_req "POST" "${WA_BASE}" "${DAVE_TOKEN}" '{"lane":"multi_device","agent_id":"'"${AGENT_ID}"'"}'
assert_status "403" "Member cannot write the WhatsApp config (403)"

# 24.2 Lane validation is server-side with field-level 422 details (task 6.3).
api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" '{}'
assert_status "422" "Lane-less WhatsApp POST is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "lane")] | length > 0' "Validation error fields lane"

api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" '{"lane":"cloud_api","agent_id":"'"${AGENT_ID}"'"}'
assert_status "422" "Cloud lane without credentials is rejected (422)"
assert_json_expr '[.error.details[]? | select(.field == "access_token")] | length > 0' "Validation error fields access_token"
assert_json_expr '[.error.details[]? | select(.field == "verify_token")] | length > 0' "Validation error fields verify_token"

api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" '{"lane":"cloud_api","agent_id":"'"${AGENT_ID}"'","transport":"long_polling"}'
assert_status "422" "Long-polling transport on the cloud lane is rejected (webhook-only)"
assert_json_expr '[.error.details[]? | select(.field == "transport")] | length > 0' "Validation error fields transport"

api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" '{"lane":"sms","agent_id":"'"${AGENT_ID}"'"}'
assert_status "422" "Unknown lane is rejected (422)"

# 24.3 Cloud config save: the four labeled write-only fields are packed into
# ONE encrypted envelope server-side (design D5); the stub probe resolves the
# display identity and nothing secret is ever echoed.
api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" "{\"lane\":\"cloud_api\",\"agent_id\":\"${AGENT_ID}\",\"access_token\":\"${WA_ACCESS_TOKEN}\",\"phone_number_id\":\"${WA_PHONE_ID}\",\"app_secret\":\"${WA_APP_SECRET}\",\"verify_token\":\"${WA_VERIFY_TOKEN}\"}" "application/json" "X-Forwarded-Proto: https"
assert_status "201" "Owner saves the cloud lane credentials"
assert_json_expr '.lane == "cloud_api"' "Cloud gateway reports the lane"
assert_json_expr '.has_credentials == true' "Config exposes only has_credentials"
assert_json_expr '.bot_username == "Smoke Cloud"' "The stub probe resolves the display identity"
assert_json_expr '.enabled == true' "A fresh cloud gateway starts enabled"
assert_json_expr '.webhook_url != null' "The webhook callback URL rides the config"
WA_GW_ID=$(json_get '.id')
if echo "${HTTP_BODY}" | grep -qE "${WA_ACCESS_TOKEN}|${WA_APP_SECRET}|${WA_VERIFY_TOKEN}|\"v1:"; then
    log_fail "WhatsApp config response echoed a credential or ciphertext envelope!"
else
    log_pass "Config response never echoes the credential envelope"
fi

api_req "GET" "${WA_BASE}/${WA_GW_ID}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads the configured WhatsApp gateway"
if echo "${HTTP_BODY}" | grep -qE "${WA_ACCESS_TOKEN}|${WA_APP_SECRET}|${WA_VERIFY_TOKEN}|\"v1:"; then
    log_fail "WhatsApp config GET echoed a credential or ciphertext envelope!"
else
    log_pass "Config GET never echoes the credential envelope"
fi

# 24.4 Meta verification handshake (GET): the stored verify token echoes the
# challenge verbatim; anything else is a bare 403 (design D8).
api_req "GET" "/api/v1/webhooks/whatsapp/${WA_GW_ID}?hub.mode=subscribe&hub.verify_token=${WA_VERIFY_TOKEN}&hub.challenge=smoke-challenge-42" ""
assert_status "200" "Verification handshake with the stored verify token returns 200"
if [[ "${HTTP_BODY}" == "smoke-challenge-42" ]]; then
    log_pass "Handshake echoes hub.challenge verbatim"
else
    log_fail "Handshake challenge echo wrong: '${HTTP_BODY}'"
fi

api_req "GET" "/api/v1/webhooks/whatsapp/${WA_GW_ID}?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=x" ""
assert_status "403" "Handshake with a wrong verify token is 403"

api_req "GET" "/api/v1/webhooks/whatsapp/${WA_GW_ID}?hub.challenge=x" ""
assert_status "403" "Handshake without hub.mode is 403"

# 24.5 Message ingress authenticates on X-Hub-Signature-256 (HMAC-SHA256 of
# the raw body under the app secret) BEFORE parsing: missing and bad
# signatures are rejected unauthenticated, for unknown workspaces too (no
# existence leak).
api_req "POST" "/api/v1/webhooks/whatsapp/${WA_GW_ID}" "" '{"object":"whatsapp_business_account"}'
assert_status "401" "Webhook POST without a signature is 401"

api_req "POST" "/api/v1/webhooks/whatsapp/${WA_GW_ID}" "" '{"object":"whatsapp_business_account"}' "application/json" "X-Hub-Signature-256: sha256=deadbeef"
assert_status "401" "Webhook POST with a bad signature is 401"

api_req "POST" "/api/v1/webhooks/whatsapp/00000000-0000-0000-0000-000000000000" "" '{"object":"whatsapp_business_account"}' "application/json" "X-Hub-Signature-256: sha256=deadbeef"
assert_status "401" "Webhook POST for an unknown gateway still demands a signature (no existence leak)"

# 24.6 Enable brings the cloud gateway up against the stub (the factory
# never calls a webhook-registration endpoint — Meta registration is
# manual, design D8) and health reports ok.
api_req "POST" "${WA_BASE}/${WA_GW_ID}/enable" "${CHARLIE_TOKEN}" '{}'
assert_status "200" "Owner enables the WhatsApp gateway"

api_req "GET" "${WA_BASE}/${WA_GW_ID}/health" "${CHARLIE_TOKEN}"
assert_status "200" "Health probe returns 200"
assert_json_expr '.status == "ok"' "Cloud health probes the running adapter (status ok)"
assert_json_expr '.detail == "Smoke Cloud · +1 555 000 1111"' "Health detail carries the stub profile"

# 24.7 Message → run → outbox delivery through the stubbed Meta endpoint
# (task 9.1): the unpaired sender pairs with /start over the webhook, then a
# marked turn runs against the mock provider and its reply is delivered back
# through the adapter's send endpoint (recorded by the stub).
api_req "POST" "${WA_BASE}/pairing-tokens" "${CHARLIE_TOKEN}" '{}'
assert_status "201" "Owner mints a WhatsApp pairing token (member-level surface)"
WA_PAIR_BODY=$(json_get '.token.token')

WA_MSG_START='{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"'${WA_PHONE_ID}'"},"contacts":[{"wa_id":"'${WA_SENDER}'","profile":{"name":"Charlie"}}],"messages":[{"from":"'${WA_SENDER}'","id":"wamid.in-start","timestamp":"1","type":"text","text":{"body":"/start '${WA_PAIR_BODY}'"}}]}}]}]}'
api_req "POST" "/api/v1/webhooks/whatsapp/${WA_GW_ID}" "" "${WA_MSG_START}" "application/json" "X-Hub-Signature-256: $(wa_sig "${WA_MSG_START}")"
assert_status "200" "Signed /start message is accepted"

WA_PAIRED=0
for i in {1..20}; do
    if grep -q "Paired" "${TMP_DIR}/wa_stub.log" 2>/dev/null; then WA_PAIRED=1; break; fi
    sleep 0.5
done
if [[ ${WA_PAIRED} -eq 1 ]]; then
    log_pass "Pairing confirmation delivered through the stub"
else
    log_fail "Pairing confirmation never reached the stub"
fi

WA_MSG_TURN='{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"'${WA_PHONE_ID}'"},"contacts":[{"wa_id":"'${WA_SENDER}'","profile":{"name":"Charlie"}}],"messages":[{"from":"'${WA_SENDER}'","id":"wamid.in-turn","timestamp":"2","type":"text","text":{"body":"ONCLAW_V1_SMOKE gateway hello"}}]}}]}]}'
api_req "POST" "/api/v1/webhooks/whatsapp/${WA_GW_ID}" "" "${WA_MSG_TURN}" "application/json" "X-Hub-Signature-256: $(wa_sig "${WA_MSG_TURN}")"
assert_status "200" "Signed chat message is accepted"

# The turn runs the workspace agent on the mock provider; the final reply is
# committed to the outbox and delivered by the background loop (write-before-
# send, design D9) — poll the stub log for the reply body.
WA_DELIVERED=0
for i in {1..60}; do
    if grep -q "live reply from the smoke mock" "${TMP_DIR}/wa_stub.log" 2>/dev/null; then WA_DELIVERED=1; break; fi
    sleep 1
done
if [[ ${WA_DELIVERED} -eq 1 ]]; then
    log_pass "Agent reply delivered to WhatsApp via the outbox (stubbed Meta endpoint)"
else
    log_fail "The agent reply never reached the stubbed Meta endpoint"
fi

# Duplicate platform message ids drop through the dedup ring: re-sending the
# turn payload must not mint a second turn/reply.
api_req "POST" "/api/v1/webhooks/whatsapp/${WA_GW_ID}" "" "${WA_MSG_TURN}" "application/json" "X-Hub-Signature-256: $(wa_sig "${WA_MSG_TURN}")"
assert_status "200" "Redelivered message payload is accepted"
sleep 3
WA_SENDS_AFTER=$(grep -c "live reply from the smoke mock" "${TMP_DIR}/wa_stub.log" 2>/dev/null || true)
if [[ "${WA_SENDS_AFTER}" -le 1 ]]; then
    log_pass "Redelivered webhook payload does not duplicate the reply"
else
    log_fail "Redelivered payload produced ${WA_SENDS_AFTER} replies (dedup ring failed)"
fi

# 24.8 Member pairing identity mirrors Telegram: mint/read/revoke are
# member-level.
api_req "GET" "${WA_BASE}/links/me" "${DAVE_TOKEN}"
assert_status "200" "Member reads their (absent) WhatsApp link"
assert_json_expr '.link == null' "Unpaired member reads a null WhatsApp link"

api_req "DELETE" "${WA_BASE}/links/me" "${DAVE_TOKEN}"
assert_status "404" "WhatsApp self unpair with no link is 404"

# 24.9 Multi-device lane: create and verify multi-device gateway account
api_req "POST" "${WA_BASE}" "${CHARLIE_TOKEN}" '{"lane":"multi_device","agent_id":"'"${AGENT_ID}"'"}'
assert_status "201" "Owner creates a WhatsApp multi-device gateway account"
assert_json_expr '.lane == "multi_device"' "Multi-device lane is stored"
assert_json_expr '.has_credentials == false' "The multi-device lane stores no credential (design D5)"
assert_json_expr '.enabled == true' "A saved multi-device config starts enabled (the pairing runtime lives on the adapter)"
assert_json_expr '.status_error == null' "Multi-device adapter started clean (no status_error on the POST)"
WA_MD_GW_ID=$(json_get '.id')

api_req "GET" "${WA_BASE}/${WA_MD_GW_ID}/pairing/status" "${CHARLIE_TOKEN}"
assert_status "200" "Pairing status answers without WhatsApp connectivity"
assert_json_expr '.status == "not_started"' "Pairing status reads not_started before any pairing attempt"

api_req "GET" "${WA_BASE}/${WA_MD_GW_ID}/health" "${CHARLIE_TOKEN}"
assert_status "200" "Multi-device health answers without WhatsApp connectivity"
assert_json_expr '.status == "error"' "Health reports the unconnected device as an error status"
assert_json_expr '.detail == "device disconnected"' "Multi-device adapter is running (health reports the live unlinked device)"
assert_json_expr '.dead_outbox == 0' "Multi-device health carries the dead-delivery signal (design D4)"

api_req "POST" "${WA_BASE}/${WA_MD_GW_ID}/pairing/logout" "${CHARLIE_TOKEN}" '{}'
assert_status "200" "Logout with no linked device is idempotent (200)"
assert_json_expr '.status == "logged_out"' "Logout answers the logged_out status"

api_req "POST" "${WA_BASE}/${WA_MD_GW_ID}/disable" "${CHARLIE_TOKEN}" '{}'
assert_status "200" "Owner disables the WhatsApp gateway"

api_req "GET" "${WA_BASE}/${WA_MD_GW_ID}" "${CHARLIE_TOKEN}"
assert_json_expr '.enabled == false' "Disabled gateway reports enabled false"
assert_json_expr '.lane == "multi_device"' "Disable preserves the lane configuration"

# -----------------------------------------------------------------------------
# 25. Agent Heartbeat: Create, Silence Contract, Channel Delivery, Active
#     Hours & Failure-Streak Auto-Pause (add-agent-heartbeat)
# -----------------------------------------------------------------------------
log_step "25. Agent Heartbeat: Run-Now, Silence, Delivery & Auto-Pause"

HB_BASE="/api/v1/workspaces/${TENANT_SLUG}/agents/hb-agent/heartbeat"
# The 5-field cadence floor is 5 minutes (domain validation); 30-minute ticks
# never come due mid-section, so the ticker's claim loop cannot race the
# manual run-nows below.
HB_EXPR="*/30 * * * *"

# 25.0 Dedicated steerable mock provider. The heartbeat checklist rides the
# tick's system prompt, so markers planted in the checklist via the PUT steer
# every tick of this section's agent: SILENT → whole-reply "NO_REPLY"
# (add-agent-heartbeat D7), REPORT → a plain report body, FAIL → an HTTP 500
# that errors the turn at generation time (a genuinely FAILED tick — not a
# pre-model blocked submit). Unmarked completions are the create/regenerate
# prompt-generation branch (the section-13 submit_prompts contract).
HB_MOCK_PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
cat > "${TMP_DIR}/hb_mock_provider.py" <<'PYHB'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1])

ARGS = json.dumps({
    "identity": "# IDENTITY.md - Who Am I?\n**Name:** Heartbeat Agent\n**Creature:** test fixture\n**Purpose:** smoke the heartbeat path\n**Vibe:** deterministic\n**Emoji:** pulse\n",
    "soul": "# SOUL.md\nShort beats long. Deterministic beats flaky.\n",
    "bootstrap": "# BOOTSTRAP.md - Birth Sequence\n_You just woke up. Keep this first conversation short and make it yours._\n",
})

REPORT = "ONCLAW_HEARTBEAT_SMOKE_REPORT disk usage at 92 percent on db-1 - needs attention."

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _sse_reply(self, reply):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def sse(part):
            self.wfile.write(("data: " + json.dumps(part) + "\n\n").encode())

        def chunk(delta, finish):
            return {
                "id": "chatcmpl-smoke-hb",
                "object": "chat.completion.chunk",
                "created": 0,
                "model": "gpt-4",
                "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
            }

        sse(chunk({"role": "assistant"}, None))
        for piece in [reply]:
            sse(chunk({"content": piece}, None))
        final = chunk({}, "stop")
        final["usage"] = {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11}
        sse(final)
        self.wfile.write(b"data: [DONE]\n\n")

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            self._send(200, {"data": [{"id": "gpt-4"}, {"id": "gpt-4o"}]})
        else:
            self._send(401, {"error": {"message": "Invalid API key"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(raw)
        except ValueError:
            body = {}
        messages = body.get("messages", [])

        def content_text(m):
            c = m.get("content")
            if isinstance(c, str):
                return c
            if isinstance(c, list):
                return " ".join(p.get("text", "") for p in c if isinstance(p, dict))
            return ""

        all_text = " ".join(content_text(m) for m in messages if isinstance(m, dict))

        if "ONCLAW_HEARTBEAT_SMOKE_FAIL" in all_text:
            self._send(500, {"error": {"message": "smoke model outage"}})
            return
        if "ONCLAW_HEARTBEAT_SMOKE_SILENT" in all_text:
            reply = "NO_REPLY"
        elif "ONCLAW_HEARTBEAT_SMOKE_REPORT" in all_text:
            reply = REPORT
        else:
            reply = None
        if reply is not None:
            if body.get("stream"):
                self._sse_reply(reply)
                return
            self._send(200, {
                "id": "chatcmpl-smoke-hb",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": reply},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 5, "completion_tokens": 6, "total_tokens": 11},
            })
            return
        if self.path.startswith("/v1/chat/completions"):
            self._send(200, {
                "id": "chatcmpl-smoke-hb-gen",
                "object": "chat.completion",
                "created": 0,
                "model": "gpt-4",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "", "tool_calls": [{
                        "id": "call_hb_smoke",
                        "type": "function",
                        "function": {"name": "submit_prompts", "arguments": ARGS},
                    }]},
                    "finish_reason": "tool_calls",
                }],
            })
        else:
            self._send(401, {"error": {"message": "Invalid API key"}})

HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PYHB
python3 "${TMP_DIR}/hb_mock_provider.py" "${HB_MOCK_PORT}" &
HB_MOCK_PID=$!
sleep 0.5
log_pass "Heartbeat mock provider listening on 127.0.0.1:${HB_MOCK_PORT}"

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Heartbeat Mock Provider","base_url":"http://127.0.0.1:'"${HB_MOCK_PORT}"'/v1","key":"sk-hb-mock-key"}'
assert_status "201" "Owner creates the heartbeat mock provider"
HB_PROV_ID=$(json_get '.provider.id')

api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" '{"name":"Heartbeat Agent","slug":"hb-agent","role":"Sentinel","description":"Watches the workspace","brief":"A heartbeat smoke fixture","provider_id":"'"${HB_PROV_ID}"'","model":"gpt-4"}'
assert_status "201" "Owner creates the heartbeat smoke agent"
HB_AGENT_ID=$(json_get '.agent.id')

# 25.1 Permission guards (add-agent-heartbeat D13): the heartbeat rides the
# agents permissions — a Member holds agents.read but not agents.write, so
# reads pass and every mutation is 403.
api_req "PUT" "${HB_BASE}" "${CLI_USER_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":null,\"active_end\":null,\"delivery\":{\"type\":\"creator_dm\"},\"prompt\":\"x\"}"
assert_status "403" "Member cannot PUT the heartbeat (agents.write 403)"
api_req "POST" "${HB_BASE}/run-now" "${CLI_USER_TOKEN}"
assert_status "403" "Member cannot run the heartbeat now (agents.write 403)"
api_req "GET" "${HB_BASE}" "${CLI_USER_TOKEN}"
assert_status "200" "Member can read the heartbeat (agents.read)"

# 25.2 Create with no prompt: the first save seeds the embedded default
# checklist template (D3) and the enabled save derives next_tick_at in the
# workspace timezone (D4).
api_req "PUT" "${HB_BASE}" "${CHARLIE_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":null,\"active_end\":null,\"delivery\":{\"type\":\"creator_dm\"}}"
assert_status "200" "Owner creates the heartbeat with a 30-minute cadence and no prompt"
assert_json_expr '.heartbeat.enabled == true' "Created heartbeat is enabled"
assert_json_expr '.heartbeat.next_tick_at != null' "Enabled heartbeat carries a derived next tick"
assert_json_expr '.heartbeat.prompt | contains("NO_REPLY")' "Empty first save seeded the default checklist template"
assert_json_expr '.heartbeat.delivery.type == "creator_dm"' "Default delivery is creator_dm"
assert_json_expr '.heartbeat.human_label | length > 0' "Write response derives the human label"

# 25.3 GET returns the stored heartbeat with its human_label plus the
# embedded template for the UI's reset-to-default affordance.
api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads the heartbeat"
assert_json_expr '.heartbeat.human_label | length > 0' "Read view derives the human label"
assert_json_expr '.heartbeat.next_tick_at != null' "Read view carries the next tick"
assert_json_expr '.default_prompt | contains("NO_REPLY")' "GET carries the embedded default template"
assert_json_expr '.heartbeat.prompt | contains("NO_REPLY")' "Stored prompt is the seeded template"

# 25.4 Silence contract (D7): a tick whose whole reply is NO_REPLY completes
# with delivery_status suppressed — no delivery surface hears anything. This
# harness has no psql access, so the zero-outbox-rows leg of the suppression
# contract is covered by the store/service unit and integration tests; the
# observable wire signal here is the suppressed delivery status.
api_req "PUT" "${HB_BASE}" "${CHARLIE_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":null,\"active_end\":null,\"delivery\":{\"type\":\"creator_dm\"},\"prompt\":\"ONCLAW_HEARTBEAT_SMOKE_SILENT routine checks: verify backups finished and report only failures.\"}"
assert_status "200" "Owner saves the silent-leg checklist"

api_req "POST" "${HB_BASE}/run-now" "${CHARLIE_TOKEN}"
assert_status "200" "Owner runs the heartbeat now"
assert_json_expr '.run.trigger == "manual"' "Run-now records trigger manual"
assert_json_expr '.run.id != null and .run.id != ""' "Run-now returns the run row"
assert_json_expr '.run.session_id | startswith("hb_")' "Tick rides the persistent hb_ session"

HB_TERMINAL=0
for i in {1..60}; do
    api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
    HB_LAST=$(json_get '.heartbeat.last_tick.status')
    if [[ "${HB_LAST}" != "null" && "${HB_LAST}" != "" ]]; then
        HB_TERMINAL=1
        break
    fi
    sleep 0.5
done
if [[ ${HB_TERMINAL} -ne 1 ]]; then
    log_fail "Silent tick never reached a terminal status (last_tick.status: ${HB_LAST})"
fi
assert_json_expr '.heartbeat.last_tick.status == "completed"' "Silent tick completed (mock produced a real agent run)"
assert_json_expr '.heartbeat.last_tick.delivery_status == "suppressed"' "Whole-reply NO_REPLY recorded delivery_status suppressed"
assert_json_expr '.heartbeat.last_tick.trigger == "manual"' "Silent tick records the manual trigger"
assert_json_expr '.heartbeat.last_tick.tokens_used > 0' "Silent tick metered token usage (the model really ran)"

# 25.5 Report delivery (D8): a non-silent reply to a channel target posts
# through the chokepoint exactly like scheduler channel delivery and records
# delivered. (The creator-DM outbox leg needs a paired gateway; the smoke
# suite's gateway sections cover outbox delivery end-to-end, so the channel
# target is the observable delivered assertion here.)
api_req "POST" "${CHANNELS_BASE}" "${CHARLIE_TOKEN}" '{"name":"Heartbeat Digest","slug":"hb-digest","purpose":"Heartbeat delivery target."}'
assert_status "201" "Owner creates the heartbeat delivery channel"
HB_CHANNEL_ID=$(json_get '.channel.id')

api_req "POST" "${CHANNELS_BASE}/${HB_CHANNEL_ID}/members" "${CHARLIE_TOKEN}" "{\"member_type\":\"agent\",\"agent_id\":\"${HB_AGENT_ID}\",\"specialization\":\"Heartbeat reports\"}"
assert_status "201" "Owner adds the heartbeat agent to the delivery channel"

api_req "PUT" "${HB_BASE}" "${CHARLIE_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":null,\"active_end\":null,\"delivery\":{\"type\":\"channel\",\"channel_id\":\"${HB_CHANNEL_ID}\"},\"prompt\":\"ONCLAW_HEARTBEAT_SMOKE_REPORT routine checks: inspect disk usage and report anything over 90 percent.\"}"
assert_status "200" "Owner switches the heartbeat to channel delivery with a report checklist"
assert_json_expr '.heartbeat.delivery.type == "channel" and .heartbeat.delivery.channel_id == "'"${HB_CHANNEL_ID}"'"' "Channel delivery target is stored"

api_req "POST" "${HB_BASE}/run-now" "${CHARLIE_TOKEN}"
assert_status "200" "Owner runs the report tick now"

HB_TERMINAL=0
for i in {1..60}; do
    api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
    HB_LAST=$(json_get '.heartbeat.last_tick.status')
    if [[ "${HB_LAST}" != "null" && "${HB_LAST}" != "" ]]; then
        HB_TERMINAL=1
        break
    fi
    sleep 0.5
done
if [[ ${HB_TERMINAL} -ne 1 ]]; then
    log_fail "Report tick never reached a terminal status (last_tick.status: ${HB_LAST})"
fi
assert_json_expr '.heartbeat.last_tick.status == "completed"' "Report tick completed"
assert_json_expr '.heartbeat.last_tick.delivery_status == "delivered"' "Report tick records the delivered channel delivery"

HB_CHAN_MSG=0
for i in {1..20}; do
    api_req "GET" "${CHANNELS_BASE}/${HB_CHANNEL_ID}/messages" "${CHARLIE_TOKEN}"
    if [[ "$(json_get '([.messages[] | select(.author_type == "agent")] | length)')" -ge 1 ]]; then
        HB_CHAN_MSG=1
        break
    fi
    sleep 0.5
done
if [[ ${HB_CHAN_MSG} -ne 1 ]]; then
    log_fail "Report delivery never posted the tick's reply into the feed"
fi
log_pass "Report delivery posted the tick's reply into the feed"
assert_json_expr '[.messages[] | select(.author_type == "agent")][0].body | contains("disk usage at 92 percent")' "Delivered report body comes from the mock provider"

# 25.6 Active hours (D5): a window that excludes the current instant in the
# workspace timezone (Europe/London, section 11) records the tick as skipped
# with the guard firing before any model call.
HB_LON_HOUR=$(TZ=Europe/London date +%H | sed 's/^0//')
HB_AH_START=$(printf '%02d:00' $(( (HB_LON_HOUR + 3) % 24 )))
HB_AH_END=$(printf '%02d:00' $(( (HB_LON_HOUR + 4) % 24 )))

api_req "PUT" "${HB_BASE}" "${CHARLIE_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":\"${HB_AH_START}\",\"active_end\":\"${HB_AH_END}\",\"delivery\":{\"type\":\"creator_dm\"},\"prompt\":\"ONCLAW_HEARTBEAT_SMOKE_SILENT routine checks: verify backups finished and report only failures.\"}"
assert_status "200" "Owner saves an active-hours window that excludes now (${HB_AH_START}-${HB_AH_END} London)"

api_req "POST" "${HB_BASE}/run-now" "${CHARLIE_TOKEN}"
assert_status "200" "Owner runs the outside-hours tick now"
assert_json_expr '.run.status == "skipped"' "Outside-hours tick is skipped (no model call, zero tokens)"
assert_json_expr '.run.delivery_status == ""' "Skipped tick carried no delivery"

api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr '.heartbeat.last_tick.status == "skipped"' "Skipped guard outcome is mirrored onto the heartbeat"
assert_json_expr '.heartbeat.enabled == true and .heartbeat.failure_streak == 0' "A skip is not a failure: streak untouched, still enabled"

# 25.7 Failure-streak auto-pause (D12): five consecutive FAILED ticks (the
# mock answers 500 at generation time, so the run genuinely fails — not a
# pre-model blocked submit) disable the heartbeat; resume re-enables it with
# a recomputed next tick and a reset streak.
api_req "PUT" "${HB_BASE}" "${CHARLIE_TOKEN}" "{\"enabled\":true,\"expr\":\"${HB_EXPR}\",\"active_start\":null,\"active_end\":null,\"delivery\":{\"type\":\"creator_dm\"},\"prompt\":\"ONCLAW_HEARTBEAT_SMOKE_FAIL routine checks: verify backups.\"}"
assert_status "200" "Owner saves the failure-leg checklist (active hours cleared)"

for f in 1 2 3 4 5; do
    HB_DISPATCHED=0
    for r in 1 2 3 4 5 6 7 8 9 10; do
        api_req "POST" "${HB_BASE}/run-now" "${CHARLIE_TOKEN}"
        if [[ "${HTTP_STATUS}" == "200" ]]; then
            HB_DISPATCHED=1
            break
        fi
        # A stale in-flight window between drain bookkeeping and the next
        # manual fire answers 409; retry until it clears.
        if [[ "${HTTP_STATUS}" != "409" ]]; then
            break
        fi
        sleep 0.5
    done
    if [[ ${HB_DISPATCHED} -ne 1 ]]; then
        log_fail "Failure run-now ${f} never dispatched (last: ${HTTP_STATUS} ${HTTP_BODY})"
    fi
    HB_STREAK_HIT=0
    for i in {1..60}; do
        api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
        if [[ "$(json_get '.heartbeat.failure_streak')" -ge "${f}" ]]; then
            HB_STREAK_HIT=1
            break
        fi
        sleep 0.5
    done
    if [[ ${HB_STREAK_HIT} -ne 1 ]]; then
        log_fail "Failure run ${f} never advanced the streak (last_tick: $(json_get '.heartbeat.last_tick.status')/$(json_get '.heartbeat.failure_streak'))"
    fi
    log_pass "Failure run ${f} recorded failed (streak: ${f})"
done

api_req "GET" "${HB_BASE}" "${CHARLIE_TOKEN}"
assert_json_expr '.heartbeat.failure_streak == 5' "Five consecutive failures reached the streak cap"
assert_json_expr '.heartbeat.enabled == false' "Five consecutive failures auto-paused the heartbeat"
assert_json_expr '.heartbeat.next_tick_at == null' "Auto-pause cleared the derived next tick"
assert_json_expr '.heartbeat.last_tick.status == "failed"' "The fifth failure is recorded on the heartbeat"

api_req "POST" "${HB_BASE}/resume" "${CHARLIE_TOKEN}"
assert_status "200" "Owner resumes the auto-paused heartbeat"
assert_json_expr '.heartbeat.enabled == true' "Resume re-enables the heartbeat"
assert_json_expr '.heartbeat.next_tick_at != null' "Resume recomputes the next tick in the workspace timezone"
assert_json_expr '.heartbeat.failure_streak == 0' "Resume resets the failure streak"

# -----------------------------------------------------------------------------
# 26. Extracted memory management (integrate-agent-zero-memory 8.4, script
#     side): the deterministic UI endpoints — notes/events listings, the
#     memory settings record with its write-only API key, the connection-test
#     failure shapes, promotion/tombstone 404 paths, and the empty morning
#     report. TODO(model-dependent): the turn-produces-chip leg, the
#     memory.search tool round-trip, and scheduled-run chip non-ingestion
#     need a steerable extraction model — leave them for the manual pass.
# -----------------------------------------------------------------------------
log_step "26. Extracted Memory: Notes, Events, Settings, Consolidate & Report"

MEM_BASE="/api/v1/workspaces/${TENANT_SLUG}/memory"

# 26.1 Permission split (tasks 5.4): reads ride membership, writes are
# workspace.write — a Member reads but cannot promote/delete/consolidate or
# touch the settings record.
api_req "GET" "${MEM_BASE}/notes" "${DAVE_TOKEN}"
assert_status "200" "Member lists memory notes (membership read)"

api_req "GET" "${MEM_BASE}/settings" "${DAVE_TOKEN}"
assert_status "200" "Member reads the memory settings view (membership read)"

api_req "PUT" "${MEM_BASE}/settings" "${DAVE_TOKEN}" '{"ingestion_enabled":false}'
assert_status "403" "Member cannot PUT memory settings (workspace.write 403)"

api_req "POST" "${MEM_BASE}/consolidate" "${DAVE_TOKEN}"
assert_status "403" "Member cannot consolidate (workspace.write 403)"

api_req "POST" "${MEM_BASE}/notes/00000000-0000-0000-0000-000000000000/promote" "${DAVE_TOKEN}" '{}'
assert_status "403" "Member cannot promote (workspace.write 403)"

api_req "DELETE" "${MEM_BASE}/notes/00000000-0000-0000-0000-000000000000" "${DAVE_TOKEN}"
assert_status "403" "Member cannot tombstone-delete (workspace.write 403)"

# 26.2 Empty listings before any pipeline output.
api_req "GET" "${MEM_BASE}/notes" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists memory notes"
assert_json_expr '.notes == []' "Notes list starts empty"
assert_json_expr '.counts.shared == 0 and .counts.user == 0 and .counts.agent == 0' "Visibility counts start at zero"

api_req "GET" "${MEM_BASE}/events" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists memory events"
assert_json_expr '.events == []' "Events list starts empty"

api_req "GET" "${MEM_BASE}/notes?visibility=shared&topic=nope&q=anything&include_tombstoned=true" "${CHARLIE_TOKEN}"
assert_status "200" "Notes filters combine without error"

# 26.3 Morning report before any consolidation: the empty shape (silence
# names itself).
api_req "GET" "${MEM_BASE}/report" "${CHARLIE_TOKEN}"
assert_status "200" "Owner reads the morning report"
assert_json_expr '.report.generated_at == null and .report.conflicts == [] and .report.merges == [] and .report.extraction_failures == 0' "Report is the explicit empty shape before consolidation"

# 26.4 Memory settings record (D16): the embedding provider IS a workspace
# provider — the record pins provider+model+dimension and no embedding
# endpoint/api_key exists. The fixture provider points at a refused port so
# the connection-test legs stay offline-deterministic.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/providers" "${CHARLIE_TOKEN}" '{"type":"openai-compatible","name":"Embedding Fixture","base_url":"http://127.0.0.1:9/v1","key":"sk-smoke-embedding-key"}'
assert_status "201" "Owner creates the embedding fixture provider"
EMBED_PROV_ID=$(json_get '.provider.id')

api_req "PUT" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}" '{"visibility_posture":"org-shared","ingestion_enabled":true,"embedding":{"provider_id":"'${EMBED_PROV_ID}'","model":"text-embedding-fixture","dimension":1536}}'
assert_status "200" "Owner PUTs the memory settings record"
assert_json_expr '.settings.visibility_posture == "org-shared"' "PUT stores the org-shared posture"
assert_json_expr '.settings.ingestion_enabled == true' "PUT stores the ingestion toggle"
assert_json_expr '.settings.embedding.provider_id == "'${EMBED_PROV_ID}'"' "Embedding record pins the workspace provider"
assert_json_expr '.settings.embedding | (has("endpoint") or has("api_key") or has("api_key_set")) | not' "Embedding record carries no endpoint or key fields"
if echo "${HTTP_BODY}" | grep -q "sk-smoke-embedding-key"; then
    log_fail "Settings PUT response leaked the embedding api key"
else
    log_pass "Settings PUT response carries no key material"
fi

api_req "GET" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}"
assert_json_expr '.settings.embedding.provider_id == "'${EMBED_PROV_ID}'" and .settings.embedding.dimension == 1536' "GET round-trips the provider-pinned embedding record"

api_req "PUT" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}" '{"embedding":{"provider_id":"00000000-0000-0000-0000-000000000000"}}'
assert_status "400" "Unknown embedding provider is invalid input (400)"

api_req "PUT" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}" '{"visibility_posture":"everyone"}'
assert_status "400" "Unknown posture is invalid input (400)"

api_req "PUT" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}" '{"embedding":{"dimension":0}}'
assert_status "400" "Non-positive dimension is invalid input (400)"

# 26.5 Connection test (D16): the probe resolves endpoint + credential from
# the pinned provider. A refused connection is a 422 naming the failure;
# missing fields and unknown providers are a 400.
api_req "POST" "${MEM_BASE}/settings/test" "${CHARLIE_TOKEN}" '{"provider_id":"'${EMBED_PROV_ID}'","model":"text-embedding-fixture"}'
assert_status "422" "Connection test surfaces the endpoint failure as 422"
assert_json_expr '.error.code == "invalid_request"' "Connection-test failure carries the structured envelope"

api_req "POST" "${MEM_BASE}/settings/test" "${CHARLIE_TOKEN}" '{"model":"text-embedding-fixture"}'
assert_status "400" "Connection test without a provider is invalid input (400)"

# 26.6 Promotion and tombstone 404 paths (unknown ids — a real note needs the
# extraction pipeline).
api_req "POST" "${MEM_BASE}/notes/00000000-0000-0000-0000-000000000000/promote" "${CHARLIE_TOKEN}" '{}'
assert_status "404" "Promoting an unknown note is 404"

api_req "DELETE" "${MEM_BASE}/notes/00000000-0000-0000-0000-000000000000" "${CHARLIE_TOKEN}"
assert_status "404" "Tombstone-deleting an unknown note is 404"

# 26.7 Restore the default posture so later sections (and reruns) start
# narrow — the fail-safe default.
api_req "PUT" "${MEM_BASE}/settings" "${CHARLIE_TOKEN}" '{"visibility_posture":"narrow"}'
assert_status "200" "Owner restores the narrow posture"
assert_json_expr '.settings.visibility_posture == "narrow"' "Posture round-trips back to narrow"

# -----------------------------------------------------------------------------
# 27. Workspace Files API (add-right-panel: agent jail file read/list with
#     path confinement and content-type serving guards)
# -----------------------------------------------------------------------------
log_step "27. Workspace Files: Jail Read/List, Path Confinement & Serving Guards"

# Dedicated agent whose jail directory this section populates directly on
# disk. The on-disk layout mirrors domain.AgentWorkspaceDir:
#   <ONCLAW_DIR>/workspaces/<workspace-slug>/agents/<agent-slug>
# WS_ROOT was computed from the same ONCLAW_DIR the server was started with,
# so the API and the filesystem below address identical bytes.
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/agents" "${CHARLIE_TOKEN}" '{"name":"Files Agent","slug":"files-agent","role":"Reporter","description":"Writes workspace files","brief":"A files fixture","provider_id":"'"${MOCK_PROV_ID}"'","model":"gpt-4"}'
assert_status "201" "Owner creates the files fixture agent"

WF_DIR="${WS_ROOT}/${TENANT_SLUG}/agents/files-agent"
WF_BASE="/api/v1/workspaces/${TENANT_SLUG}/agents/files-agent/files"
mkdir -p "${WF_DIR}/reports"
printf '# Smoke Report\n\nAuthored by the smoke suite.\n' > "${WF_DIR}/notes.md"
printf '<!DOCTYPE html><html><body><script>alert("smoke")</script></body></html>' > "${WF_DIR}/page.html"
printf '<svg xmlns="http://www.w3.org/2000/svg"><circle r="1"/></svg>' > "${WF_DIR}/logo.svg"
printf '# Q3\n' > "${WF_DIR}/reports/q3.md"

# Helper: assert the LAST response carried a header line (value regex).
assert_header() {
    local name="$1"
    local value_regex="$2"
    local msg="$3"
    if grep -qi "^${name}:${value_regex}" "${TMP_DIR}/headers.tmp"; then
        log_pass "${msg}"
    else
        log_fail "${msg}: no ${name} header matching /${value_regex}/ in the response headers"
    fi
}

assert_no_header() {
    local name="$1"
    local msg="$2"
    if grep -qi "^${name}:" "${TMP_DIR}/headers.tmp"; then
        log_fail "${msg}: unexpected ${name} header present"
    else
        log_pass "${msg}"
    fi
}

# 27.1 Read the authored markdown: exact bytes, nosniff, no attachment.
api_req "GET" "${WF_BASE}?path=notes.md" "${CHARLIE_TOKEN}"
assert_status "200" "Read authored markdown file (200)"
WF_EXPECTED=$(printf '# Smoke Report\n\nAuthored by the smoke suite.\n')
if [[ "${HTTP_BODY}" == "${WF_EXPECTED}" ]]; then
    log_pass "Markdown read returns the authored bytes"
else
    log_fail "Markdown read bytes differ from the authored file"
fi
assert_header "X-Content-Type-Options" "[[:space:]]*nosniff" "Markdown read carries nosniff"
assert_no_header "Content-Disposition" "Display-safe markdown serves without attachment disposition"

# 27.2 Path confinement: traversal and absolute paths collapse to not found.
api_req "GET" "${WF_BASE}?path=../../other-agent/secrets.md" "${CHARLIE_TOKEN}"
assert_status "404" "Parent traversal request is not found"
api_req "GET" "${WF_BASE}?path=/etc/passwd" "${CHARLIE_TOKEN}"
assert_status "404" "Absolute path request is not found"
api_req "GET" "${WF_BASE}?path=missing.md" "${CHARLIE_TOKEN}"
assert_status "404" "Missing file is not found"

# 27.3 Stored HTML and SVG never render inline: attachment + nosniff.
api_req "GET" "${WF_BASE}?path=page.html" "${CHARLIE_TOKEN}"
assert_status "200" "Read stored HTML file (200)"
assert_header "Content-Disposition" ".*attachment" "Stored HTML carries attachment disposition"
assert_header "X-Content-Type-Options" "[[:space:]]*nosniff" "HTML read carries nosniff"

api_req "GET" "${WF_BASE}?path=logo.svg" "${CHARLIE_TOKEN}"
assert_status "200" "Read stored SVG file (200)"
assert_header "Content-Disposition" ".*attachment" "Stored SVG carries attachment disposition"

# 27.4 List mode: exactly one directory level per response, nosniff on
# listings too (the jail also holds the agent's generated IDENTITY/SOUL/
# BOOTSTRAP prompt documents — assert by membership, not exact set).
api_req "GET" "${WF_BASE}?mode=list" "${CHARLIE_TOKEN}"
assert_status "200" "List the agent workspace root (200)"
assert_json_expr '(.entries | map(select(.name == "notes.md" and .kind == "file")) | length) == 1' "Root listing contains notes.md as a file"
assert_json_expr '(.entries | map(select(.name == "reports" and .kind == "directory")) | length) == 1' "Root listing contains reports as a directory"
assert_json_expr '(.entries | map(select(.name == "q3.md")) | length) == 0' "Root listing stays at one level (no q3.md)"
assert_header "X-Content-Type-Options" "[[:space:]]*nosniff" "Listing carries nosniff"

api_req "GET" "${WF_BASE}?path=reports&mode=list" "${CHARLIE_TOKEN}"
assert_status "200" "List a nested directory (200)"
assert_json_expr '(.entries | length) == 1 and .entries[0].name == "q3.md" and .entries[0].kind == "file"' "Nested listing shows only that directory's children"

api_req "GET" "${WF_BASE}?path=notes.md&mode=list" "${CHARLIE_TOKEN}"
assert_status "404" "Listing a regular file is not found"

# -----------------------------------------------------------------------------
# 28. Workspace service connections (add-workspace-connections: recipe gallery,
#     integrations.write guards, connect validation envelopes, the real
#     upstream probe gate, and disconnect-cascade hygiene)
# -----------------------------------------------------------------------------
log_step "28. Workspace Service Connections: Recipes, Guards, Probe Gate"

# The connect flow is probe-gated against the recipe's declared endpoint
# (frozen server-side data, design D7) — with no live GitHub/GitLab PAT in a
# smoke run the happy-path lifecycle (connect → attach → disconnect) is
# covered by the fake-based HTTP tests (internal/server/connections_test.go).
# This section exercises everything that is deterministic here: the gallery,
# the permission tier, the validation envelopes, and the probe gate itself —
# a bogus token against the real upstream fails the gate in every environment
# (offline: dial error; online: upstream 401), stores NOTHING, and surfaces
# the upstream message (spec: "Probe failure blocks connect").
CONN_BASE="/api/v1/workspaces/${TENANT_SLUG}/integrations"

# 28.1 The recipe registry: gallery order, GitHub available with guided steps,
# scopes, and the probe declaration; Atlassian declared coming-soon.
api_req "GET" "${CONN_BASE}/recipes" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists the integration recipes"
assert_json_expr '(.recipes | length) >= 5' "The registry declares the built-in services"
assert_json_expr '[.recipes[] | select(.id == "github")][0].availability == "available"' "GitHub recipe is available"
assert_json_expr '[.recipes[] | select(.id == "github")][0].auth_kind == "pat"' "GitHub recipe declares PAT auth"
assert_json_expr '[.recipes[] | select(.id == "github")][0].transport == "streamable_http"' "GitHub recipe declares the streamable HTTP transport"
assert_json_expr '[.recipes[] | select(.id == "github")][0].endpoint != ""' "GitHub recipe carries its endpoint"
assert_json_expr '[.recipes[] | select(.id == "github")][0].probe.tool == "list_repositories"' "GitHub recipe declares its probe tool"
assert_json_expr '[.recipes[] | select(.id == "github")][0].steps | length >= 3' "GitHub recipe carries guided setup steps"
assert_json_expr '[.recipes[] | select(.id == "github")][0].access_levels[0] == "read_only"' "GitHub recipe defaults to read-only"
assert_json_expr '[.recipes[] | select(.id == "github")][0].scopes | length >= 2' "GitHub recipe carries per-level scope guidance"
assert_json_expr '[.recipes[] | select(.id == "atlassian")][0].availability == "coming_soon"' "Atlassian recipe is declared coming-soon"
assert_json_expr '[.recipes[] | select(.id == "atlassian")][0].notes != ""' "Coming-soon recipes carry truthful copy"

# 28.2 Permission tier: reads ride membership; connect/probe/disconnect are
# integrations.write — the Member is 403 on every manage verb.
api_req "GET" "${CONN_BASE}/recipes" "${CLI_USER_TOKEN}"
assert_status "200" "Member reads the recipes gallery (membership)"
api_req "GET" "${CONN_BASE}/connections" "${CLI_USER_TOKEN}"
assert_status "200" "Member reads the connections list (membership)"
assert_json_expr '(.connections | length) == 0' "Connections start empty"

api_req "POST" "${CONN_BASE}/connections" "${CLI_USER_TOKEN}" '{"recipe_id":"github","access_level":"read_only","token":"tok"}'
assert_status "403" "Member cannot connect a service (integrations.write 403)"
assert_json_expr '.error.code == "forbidden"' "Connect rejection is forbidden"

# 28.3 Connect validation envelopes: unknown recipe, coming-soon recipe, bad
# access level, and an empty token are all 400 invalid_request — and none of
# them reach the probe or the store.
api_req "POST" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}" '{"recipe_id":"not-a-service","token":"tok"}'
assert_status "400" "Unknown recipe is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Unknown recipe code is invalid_request"

api_req "POST" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}" '{"recipe_id":"atlassian","token":"tok"}'
assert_status "400" "Coming-soon recipe accepts no connect requests (400)"

api_req "POST" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}" '{"recipe_id":"github","access_level":"root","token":"tok"}'
assert_status "400" "Access level outside the recipe's offer is rejected (400)"

api_req "POST" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}" '{"recipe_id":"github","access_level":"read_only","token":"   "}'
assert_status "400" "Empty token is rejected (400)"

# 28.4 The probe gate with a real upstream: a well-formed connect whose token
# cannot pass the recipe's probe is rejected 400 with the upstream message
# verbatim after the "probe failed: " prefix — and stores nothing.
api_req "POST" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}" '{"recipe_id":"github","access_level":"read_only","token":"ghp_smoke-invalid-token"}'
assert_status "400" "A token failing the recipe probe is rejected (400)"
assert_json_expr '.error.code == "invalid_request"' "Probe-failure code is invalid_request"
assert_json_expr '.error.message | startswith("probe failed: ")' "Probe-failure message carries the upstream error verbatim"
CONN_PROBE_MSG=$(json_get '.error.message')
if [[ "${#CONN_PROBE_MSG}" -gt 20 ]]; then
    log_pass "Upstream probe failure message is substantive: ${CONN_PROBE_MSG:0:60}..."
else
    log_fail "Upstream probe failure message is too short to be real: ${CONN_PROBE_MSG}"
fi

api_req "GET" "${CONN_BASE}/connections" "${CHARLIE_TOKEN}"
assert_status "200" "Owner lists connections after the failed connect"
assert_json_expr '(.connections | length) == 0' "Probe failure stored no connection (nothing-stored hygiene)"
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/mcp-servers" "${CHARLIE_TOKEN}"
assert_json_expr '[.servers[] | select(.name == "GitHub")] | length == 0' "Probe failure materialized no server row"

# 28.5 Read paths for an unknown connection are 404 (get, probe, disconnect).
api_req "GET" "${CONN_BASE}/connections/00000000-0000-0000-0000-000000000000" "${CHARLIE_TOKEN}"
assert_status "404" "Unknown connection get is 404"
api_req "POST" "${CONN_BASE}/connections/00000000-0000-0000-0000-000000000000/probe" "${CHARLIE_TOKEN}"
assert_status "404" "Unknown connection probe is 404"
api_req "DELETE" "${CONN_BASE}/connections/00000000-0000-0000-0000-000000000000" "${CHARLIE_TOKEN}"
assert_status "404" "Unknown connection disconnect is 404"
