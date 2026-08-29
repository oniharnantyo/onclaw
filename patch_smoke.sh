#!/bin/bash
sed -i '' '/log_step "Smoke Test Suite Completed Successfully"/i \
# -----------------------------------------------------------------------------\
# 11. Admin Tenant Control\
# -----------------------------------------------------------------------------\
log_step "11. Admin Tenant Control"\
\
# 11.1 Patch tenant details\
api_req "PATCH" "/api/v1/admin/workspaces/${TENANT_SLUG}" "${SUPERADMIN_TOKEN}" "{\"name\":\"Renamed Smoke Tenant\", \"timezone\":\"Europe/London\"}"\
assert_status "200" "Admin patches workspace name and timezone"\
assert_json_expr ".workspace.name == \\"Renamed Smoke Tenant\\"" "Workspace name was updated"\
assert_json_expr ".workspace.timezone == \\"Europe/London\\"" "Workspace timezone was updated"\
\
# 11.2 Admin lists tenant members\
api_req "GET" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}"\
assert_status "200" "Admin lists tenant members"\
assert_json_expr "(.members | length) >= 2" "Admin sees tenant members"\
\
# 11.3 Admin adds member to tenant (Charlie as Member)\
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"\
assert_status "201" "Admin adds Charlie to tenant as Member"\
\
# Guard: Admin attempts to add user with Owner role -> 400\
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CLI_USER_EMAIL}\",\"role_id\":\"${OWNER_ROLE_ID}\"}"\
assert_status "400" "Admin adding member with Owner role is rejected"\
\
# Guard: Admin attempts to add unknown user -> 404\
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"nobody@nowhere.test\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"\
assert_status "404" "Admin adding unknown user is rejected"\
\
# Guard: Admin attempts to add already-member -> 409\
api_req "POST" "/api/v1/admin/workspaces/${TENANT_SLUG}/members" "${SUPERADMIN_TOKEN}" "{\"email\":\"${CHARLIE_EMAIL}\",\"role_id\":\"${MEMBER_ROLE_ID}\"}"\
assert_status "409" "Admin adding already-member is rejected"\
\
# 11.4 Admin transfers ownership (Alice -> Charlie)\
api_req "PATCH" "/api/v1/admin/workspaces/${TENANT_SLUG}/owner" "${SUPERADMIN_TOKEN}" "{\"user_id\":\"${CHARLIE_UID}\"}"\
assert_status "200" "Admin transfers ownership to Charlie"\
\
' ./scripts/smoke.sh
