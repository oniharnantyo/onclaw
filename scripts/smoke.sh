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
#
# Usage:
#   ./scripts/smoke.sh
#   DATABASE_URL="postgres://user:pass@localhost:5432/onclaw?sslmode=disable" ./scripts/smoke.sh
#   SERVER_URL="http://localhost:8080" ./scripts/smoke.sh
# ==============================================================================

set -euo pipefail

# Configuration with sensible defaults
SERVER_PORT="${SERVER_PORT:-8088}"
SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
SERVER_URL="${SERVER_URL:-http://${SERVER_HOST}:${SERVER_PORT}}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres@127.0.0.1:5432/onclaw_smoke?sslmode=disable}"
SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-admin@onclaw.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD:-SmokeSuperAdminSecret123!}"
JWT_SECRET="${JWT_SECRET:-smoke-test-jwt-secret-at-least-32-chars-long!}"

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

SERVER_PID=""

cleanup() {
    local exit_code=$?
    if [[ -n "${SERVER_PID}" ]]; then
        log_info "Stopping background server (PID: ${SERVER_PID})..."
        kill "${SERVER_PID}" 2>/dev/null || true
        wait "${SERVER_PID}" 2>/dev/null || true
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

    # Start server with env-seeded superadmin
    DATABASE_URL="${DATABASE_URL}" \
    ONCLAW_SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL}" \
    ONCLAW_SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD}" \
    ONCLAW_JWT_SECRET="${JWT_SECRET}" \
    ONCLAW_DATA_DIR="${DATA_DIR}" \
    ONCLAW_LISTEN_ADDR="${SERVER_HOST}:${SERVER_PORT}" \
    go run . server --database-url "${DATABASE_URL}" --listen-addr "${SERVER_HOST}:${SERVER_PORT}" >"${TMP_DIR}/server.log" 2>&1 &

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

log_step "Smoke Test Suite Completed Successfully"

