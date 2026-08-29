## Purpose

Instance-level administration via a default master tenant: the control plane is ordinary domain data (a workspace, a role, permission strings) so superadmins reuse the same roles, guards, and middleware as everyone else, and a fresh instance seeds its first superadmin from the environment.

## ADDED Requirements

### Requirement: Master tenant seeded by default
The system SHALL ensure a master tenant exists on every instance: reserved slug `master`, flagged `is_master`, with built-in roles `Superadmin` (owner authority, admin.* permissions) and `Member` (reads). Ensuring SHALL be idempotent.

#### Scenario: Fresh instance
- **WHEN** a fresh instance starts
- **THEN** the master tenant exists with exactly the Superadmin and Member built-in roles and no members

#### Scenario: Master is protected
- **WHEN** a superadmin attempts to suspend or rename the master tenant
- **THEN** the request is rejected

#### Scenario: Reserved slug
- **WHEN** anyone attempts to create a workspace with slug `master`
- **THEN** response is 400 invalid_request (reserved)

### Requirement: Superadmin seeding from environment
On server start, if `ONCLAW_SUPERADMIN_EMAIL` and (`ONCLAW_SUPERADMIN_PASSWORD` or `ONCLAW_SUPERADMIN_PASSWORD_FILE`) are set and the master tenant has no Superadmin-role member, the system SHALL create the user and their Superadmin membership in the master tenant in one transaction. Otherwise the variables SHALL be ignored with a log line. The password SHALL never be logged.

#### Scenario: First boot with env
- **WHEN** a fresh instance starts with the superadmin env variables set
- **THEN** the user exists, is a Superadmin in the master tenant, and can log in

#### Scenario: Idempotent re-seed
- **WHEN** the server restarts with the same env set
- **THEN** seeding is skipped and a log line says so; existing users are unchanged

#### Scenario: No env configured
- **WHEN** the instance starts without the superadmin env variables
- **THEN** no superadmin is created; admin routes remain inert until one exists

### Requirement: Admin API surface
Routes under `/api/v1/admin/*` SHALL require membership in the master tenant plus the relevant `admin.*` permission. No new authentication mechanism is introduced.

#### Scenario: Guarded
- **WHEN** a non-superadmin calls an admin route
- **THEN** response is 403 (or 404 when not a master-tenant member)

### Requirement: Tenant management by superadmin
With `admin.workspaces.read`/`admin.workspaces.write`, a superadmin SHALL list all workspaces (including suspended), create a workspace and assign an owner by email (unknown email auto-creates a password-less account, same as direct-add), and suspend/restore any workspace except the master.

#### Scenario: List all tenants
- **WHEN** a superadmin lists workspaces
- **THEN** all workspaces are returned, including suspended ones, with member counts

#### Scenario: Create tenant and assign owner
- **WHEN** a superadmin creates a workspace with an owner email of an existing user
- **THEN** the workspace exists with its built-in roles and that user is Owner

#### Scenario: Assign owner by unknown email
- **WHEN** the owner email does not match any user
- **THEN** the account is created (no password) and assigned Owner

#### Scenario: Suspend and restore
- **WHEN** a superadmin suspends a workspace and then restores it
- **THEN** suspended tenants reject member requests with 403 (members keep membership), and restore returns access

#### Scenario: Master cannot be suspended
- **WHEN** the master tenant is targeted for suspension
- **THEN** response is 400 invalid_request

### Requirement: User management by superadmin
With `admin.users.read`/`admin.users.write`, a superadmin SHALL list users, create users, and disable/enable users globally.

#### Scenario: Global disable
- **WHEN** a superadmin disables a user
- **THEN** the user is rejected at login and all authenticated requests return 401 across every workspace

### Requirement: Superadmin self-management
With `admin.superadmins.write`, a superadmin SHALL grant or revoke the Superadmin role in the master tenant. The last-admin guard (owner authority) SHALL prevent removing the last superadmin.

#### Scenario: Promote and demote
- **WHEN** a superadmin grants the Superadmin role to a user and later revokes it
- **THEN** membership changes take effect on their next request

#### Scenario: Last superadmin protected
- **WHEN** the only Superadmin-role holder is demoted or removed
- **THEN** response is 409 last_owner_protected

### Requirement: No cross-tenant powers by default
Inside any non-master workspace a superadmin SHALL be an ordinary user: tenant routes enforce membership identically, with no implicit bypass.

#### Scenario: No bypass
- **WHEN** a superadmin who is not a member of workspace `acme` requests its members
- **THEN** response is 404, identical to any non-member
