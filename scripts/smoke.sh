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
#  14. /v1 OpenResponses live chat sessions (chat key exchange → birth → chain)
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

    curl "${args[@]}" "${url}" || log_fail "curl failed to reach ${url}"

    # Extract HTTP status code
    HTTP_STATUS=$(head -n 1 "${headers_file}" | awk '{print $2}')
    HTTP_BODY=$(cat "${body_file}")
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

    HTTP_STATUS=$(head -n 1 "${headers_file}" | awk '{print $2}')
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

        is_live = any(
            "ONCLAW_V1_SMOKE" in content_text(m)
            for m in messages
            if isinstance(m, dict)
        )
        if is_live:
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
                sse(chunk({}, "stop"))
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

# The agent workspace directory is created and seeded with the L1 base prompt
# at create time; generated documents (IDENTITY/SOUL/BOOTSTRAP.md) land in the
# same directory once generation succeeds against a live provider.
AGENT_WS_DIR="${WS_ROOT}/${TENANT_SLUG}/agents/test-agent"
if [[ -d "${AGENT_WS_DIR}" ]]; then
    log_pass "Agent workspace directory exists (${AGENT_WS_DIR})"
else
    log_fail "Agent workspace directory missing: ${AGENT_WS_DIR}"
fi
if [[ -f "${AGENT_WS_DIR}/AGENTS.md" ]]; then
    log_pass "Agent workspace seeded with AGENTS.md base prompt"
else
    log_fail "Agent workspace missing AGENTS.md base prompt"
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

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/web.search" "${CHARLIE_TOKEN}" '{"enabled":true,"config":{"provider":"tavily"}}'
assert_status "422" "Enabling an unconfigured provider returns 422"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/tools/web.search" "${CHARLIE_TOKEN}" '{"enabled":true,"config":{"provider":"tavily","api_key":"tvly-smoke-secret-key-9876"}}'
assert_status "200" "Configured provider enables"
assert_json_expr '.tool.configured == true' "Configured tool reports configured"
if echo "${HTTP_BODY}" | grep -q "tvly-smoke-secret-key-9876"; then
    log_fail "Tool config response echoed the secret"
else
    log_pass "Tool config response never echoes the secret"
fi

# 13.3 Delete in-use provider 409 (the agent references the mock provider)
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/providers/${MOCK_PROV_ID}" "${CHARLIE_TOKEN}"
assert_status "409" "Deleting in-use provider returns 409"

# 13.4 Memory view/reset
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/memory" "${CHARLIE_TOKEN}"
assert_status "200" "View agent memory"

api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/agents/${AGENT_ID}/memory" "${CHARLIE_TOKEN}"
assert_status "204" "Reset agent memory"

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

