#!/usr/bin/env bash
# ==============================================================================
# integrate-web-identity: 6.3 Manual e2e verification script
# Simulates full web frontend client lifecycle against live backend:
#   1. Login (POST /auth/login)
#   2. Switcher hydration (GET /auth/me, GET /workspaces)
#   3. Create Tenant (POST /workspaces & POST /admin/workspaces)
#   4. Members pane operations (GET/POST/PATCH members, roles)
#   5. Guards validation (peer guard 403, last_owner_protected 409)
#   6. Workspace suspend/restore & access enforcement
#   7. Superadmin promote/demote & last-admin guard
#   8. Logout (POST /auth/logout)
# ==============================================================================

set -euo pipefail

SERVER_PORT="${SERVER_PORT:-8089}"
SERVER_HOST="${SERVER_HOST:-127.0.0.1}"
SERVER_URL="http://${SERVER_HOST}:${SERVER_PORT}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres@127.0.0.1:5432/onclaw_smoke?sslmode=disable}"
SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL:-admin@onclaw.local}"
SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD:-SmokeSuperAdminSecret123!}"
JWT_SECRET="${JWT_SECRET:-verify-e2e-jwt-secret-at-least-32-chars-long!}"

C_RESET="\033[0m"
C_RED="\033[31m"
C_GREEN="\033[32m"
C_YELLOW="\033[33m"
C_BLUE="\033[34m"
C_CYAN="\033[36m"
C_BOLD="\033[1m"

log_info() { printf "${C_BLUE}ℹ  %s${C_RESET}\n" "$1"; }
log_step() { printf "\n${C_BOLD}${C_CYAN}=== %s ===${C_RESET}\n" "$1"; }
log_pass() { printf "${C_GREEN}✓ PASS:${C_RESET} %s\n" "$1"; }
log_fail() { printf "${C_RED}✗ FAIL:${C_RESET} %s\n" "$1" >&2; exit 1; }

TMP_DIR=$(mktemp -d "/tmp/onclaw-verify-e2e.XXXXXX")
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
        printf "\n${C_BOLD}${C_GREEN}🎉 All 6.3 E2E verification flows passed successfully!${C_RESET}\n\n"
    else
        printf "\n${C_BOLD}${C_RED}💥 6.3 E2E verification failed!${C_RESET}\n\n"
    fi
}
trap cleanup EXIT INT TERM

HTTP_STATUS=""
HTTP_BODY=""

api_req() {
    local method="$1"
    local path="$2"
    local token="${3:-}"
    local body="${4:-}"

    local url="${SERVER_URL}${path}"
    local headers_file="${TMP_DIR}/headers.tmp"
    local body_file="${TMP_DIR}/body.tmp"

    local args=(-s -S -X "${method}" -D "${headers_file}" -o "${body_file}")
    if [[ -n "${token}" ]]; then
        args+=(-H "Authorization: Bearer ${token}")
    fi
    if [[ -n "${body}" ]]; then
        args+=(-H "Content-Type: application/json" --data "${body}")
    fi

    curl "${args[@]}" "${url}" || log_fail "curl failed to reach ${url}"
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

# --- Launch Server ---
log_step "Starting Live Backend on port ${SERVER_PORT}"
go run . migrate up --database-url "${DATABASE_URL}" >/dev/null

DATABASE_URL="${DATABASE_URL}" \
ONCLAW_SUPERADMIN_EMAIL="${SUPERADMIN_EMAIL}" \
ONCLAW_SUPERADMIN_PASSWORD="${SUPERADMIN_PASSWORD}" \
ONCLAW_JWT_SECRET="${JWT_SECRET}" \
ONCLAW_DATA_DIR="${DATA_DIR}" \
ONCLAW_LISTEN_ADDR="${SERVER_HOST}:${SERVER_PORT}" \
go run . server --database-url "${DATABASE_URL}" --listen-addr "${SERVER_HOST}:${SERVER_PORT}" >"${TMP_DIR}/server.log" 2>&1 &

SERVER_PID=$!
READY=0
for i in {1..30}; do
    if curl -s -f "${SERVER_URL}/healthz" >/dev/null 2>&1; then
        READY=1
        break
    fi
    sleep 0.3
done
if [[ $READY -ne 1 ]]; then
    log_fail "Server failed to start. Logs:\n$(cat "${TMP_DIR}/server.log")"
fi
log_pass "Server ready on ${SERVER_URL}"

RUN_ID="$(date +%s)-${RANDOM}"

# 1. Login
log_step "1. Flow: Login"
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${SUPERADMIN_EMAIL}\",\"password\":\"${SUPERADMIN_PASSWORD}\"}"
assert_status "200" "Superadmin login"
ADMIN_TOKEN=$(json_get '.token')
ADMIN_UID=$(json_get '.user.id')

# Create test user UserOne
U1_EMAIL="user1.${RUN_ID}@example.com"
U1_PASS="UserOnePass123!"
api_req "POST" "/api/v1/admin/users" "${ADMIN_TOKEN}" "{\"email\":\"${U1_EMAIL}\",\"name\":\"User One\",\"password\":\"${U1_PASS}\"}"
assert_status "201" "Create UserOne"
U1_UID=$(json_get '.user.id')

api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${U1_EMAIL}\",\"password\":\"${U1_PASS}\"}"
assert_status "200" "UserOne login"
U1_TOKEN=$(json_get '.token')

# 2. Switcher Hydration
log_step "2. Flow: Switcher Hydration"
api_req "GET" "/api/v1/auth/me" "${U1_TOKEN}"
assert_status "200" "UserOne GET /auth/me"
assert_json_expr ".user.email == \"${U1_EMAIL}\"" "Correct user authenticated"

api_req "GET" "/api/v1/workspaces" "${U1_TOKEN}"
assert_status "200" "UserOne GET /workspaces"
assert_json_expr '(.workspaces | length) == 0' "Initial workspaces empty for new user"

# 3. Create Tenant
log_step "3. Flow: Create Tenant"
TENANT_SLUG="e2e-tenant-${RUN_ID}"
api_req "POST" "/api/v1/workspaces" "${U1_TOKEN}" "{\"name\":\"E2E Workspace\",\"slug\":\"${TENANT_SLUG}\",\"timezone\":\"America/New_York\"}"
assert_status "201" "UserOne creates workspace via POST /workspaces"
assert_json_expr '.role.is_owner == true' "Creator assigned Owner role"

api_req "GET" "/api/v1/workspaces" "${U1_TOKEN}"
assert_status "200" "UserOne GET /workspaces reflects new workspace"
assert_json_expr '(.workspaces | length) == 1' "Workspace switcher has 1 membership"
assert_json_expr ".workspaces[0].workspace_slug == \"${TENANT_SLUG}\"" "Workspace slug matches"
assert_json_expr '.workspaces[0].role_name == "Owner"' "Role badge is Owner"

# 4. Members Pane & Roles
log_step "4. Flow: Members Pane & Role Operations"
api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/roles" "${U1_TOKEN}"
assert_status "200" "List workspace roles"
OWNER_ROLE_ID=$(json_get '.roles[] | select(.name == "Owner") | .id')
ADMIN_ROLE_ID=$(json_get '.roles[] | select(.name == "Admin") | .id')
MEMBER_ROLE_ID=$(json_get '.roles[] | select(.name == "Member") | .id')

# Create and invite UserTwo
U2_EMAIL="user2.${RUN_ID}@example.com"
U2_PASS="UserTwoPass123!"
api_req "POST" "/api/v1/admin/users" "${ADMIN_TOKEN}" "{\"email\":\"${U2_EMAIL}\",\"name\":\"User Two\",\"password\":\"${U2_PASS}\"}"
assert_status "201" "Create UserTwo"
U2_UID=$(json_get '.user.id')

# Create and invite UserThree
U3_EMAIL="user3.${RUN_ID}@example.com"
U3_PASS="UserThreePass123!"
api_req "POST" "/api/v1/admin/users" "${ADMIN_TOKEN}" "{\"email\":\"${U3_EMAIL}\",\"name\":\"User Three\",\"password\":\"${U3_PASS}\"}"
assert_status "201" "Create UserThree"
U3_UID=$(json_get '.user.id')

# Invite UserTwo as Member
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${U1_TOKEN}" "{\"email\":\"${U2_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Invite UserTwo to workspace as Member"

# Invite UserThree as Member
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${U1_TOKEN}" "{\"email\":\"${U3_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "201" "Invite UserThree to workspace as Member"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}/members" "${U1_TOKEN}"
assert_status "200" "List members returns 3 members"
assert_json_expr '(.members | length) == 3' "3 members present"

# Promote UserTwo and UserThree to Admin
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${U2_UID}" "${U1_TOKEN}" "{\"role_id\":\"${ADMIN_ROLE_ID}\"}"
assert_status "200" "Owner promotes UserTwo to Admin"

api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${U3_UID}" "${U1_TOKEN}" "{\"role_id\":\"${ADMIN_ROLE_ID}\"}"
assert_status "200" "Owner promotes UserThree to Admin"

# 5. Guards Validation
log_step "5. Flow: Guards Validation"
api_req "POST" "/api/v1/auth/login" "" "{\"email\":\"${U2_EMAIL}\",\"password\":\"${U2_PASS}\"}"
assert_status "200" "UserTwo login"
U2_TOKEN=$(json_get '.token')

# Guard: UserTwo (Admin) cannot assign Owner role -> 403 Forbidden (canAssign guard)
api_req "POST" "/api/v1/workspaces/${TENANT_SLUG}/members" "${U2_TOKEN}" "{\"email\":\"someone@example.com\",\"role_id\":\"${OWNER_ROLE_ID}\"}"
assert_status "403" "Admin cannot assign Owner role (403 Forbidden)"

# Guard: UserTwo (Admin) cannot modify peer Admin UserThree -> 403 Forbidden (canEdit guard)
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${U3_UID}" "${U2_TOKEN}" "{\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "403" "Admin cannot demote peer Admin (403 Forbidden)"

# Guard: UserOne (Sole Owner) cannot demote herself (last_owner_protected)
api_req "PATCH" "/api/v1/workspaces/${TENANT_SLUG}/members/${U1_UID}" "${U1_TOKEN}" "{\"role_id\":\"${MEMBER_ROLE_ID}\"}"
assert_status "409" "Sole owner demote rejected (409 last_owner_protected)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# Guard: UserOne (Sole Owner) cannot leave/delete herself (last_owner_protected)
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/members/${U1_UID}" "${U1_TOKEN}"
assert_status "409" "Sole owner leave rejected (409 last_owner_protected)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# Cleanup UserThree
api_req "DELETE" "/api/v1/workspaces/${TENANT_SLUG}/members/${U3_UID}" "${U1_TOKEN}"
assert_status "204" "Owner removes UserThree (204 No Content)"

# 6. Suspend and Restore
log_step "6. Flow: Suspend and Restore"
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/disable" "${ADMIN_TOKEN}"
assert_status "200" "Admin suspends workspace"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${U2_TOKEN}"
assert_status "403" "Member accessing suspended workspace receives 403"
assert_json_expr '.error.code == "forbidden"' "Error code is forbidden"

api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/enable" "${ADMIN_TOKEN}"
assert_status "200" "Admin restores workspace"

api_req "GET" "/api/v1/workspaces/${TENANT_SLUG}" "${U2_TOKEN}"
assert_status "200" "Member accessing restored workspace receives 200"

# 7. Superadmin Promote / Demote
log_step "7. Flow: Superadmin Promote / Demote & Last-Admin Guard"
# Promote UserOne to Superadmin
api_req "POST" "/api/v1/admin/superadmins" "${ADMIN_TOKEN}" "{\"user_id\":\"${U1_UID}\"}"
assert_status "201" "Promote UserOne to Superadmin in master workspace"

# Demote UserOne
api_req "DELETE" "/api/v1/admin/superadmins/${U1_UID}" "${ADMIN_TOKEN}"
assert_status "204" "Demote UserOne from Superadmin"

# Demote last superadmin -> 409 last_owner_protected
api_req "DELETE" "/api/v1/admin/superadmins/${ADMIN_UID}" "${ADMIN_TOKEN}"
assert_status "409" "Demoting only remaining superadmin rejected (409 last_owner_protected)"
assert_json_expr '.error.code == "last_owner_protected"' "Error code is last_owner_protected"

# 8. Logout
log_step "8. Flow: Logout"
api_req "POST" "/api/v1/auth/logout" "${U1_TOKEN}"
assert_status "204" "UserOne logout (204 No Content)"

api_req "POST" "/api/v1/auth/logout" "${ADMIN_TOKEN}"
assert_status "204" "Superadmin logout (204 No Content)"

log_step "All 6.3 E2E verification steps passed successfully!"
