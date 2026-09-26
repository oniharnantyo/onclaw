# instance-admin Specification

## Purpose

Instance-level administration via a default master tenant: the control plane is ordinary domain data (a workspace, a role, permission strings) so superadmins reuse the same roles, guards, and middleware as everyone else, and a fresh instance seeds its first superadmin from the environment.

## Requirements

### Requirement: Master tenant seeded by default
The system SHALL ensure a master tenant exists on every instance: reserved slug `master`, flagged `is_master`, with built-in roles `Superadmin` (owner authority, admin.* permissions) and `Member` (reads). Ensuring SHALL be idempotent.

#### Scenario: Fresh instance
- **WHEN** a fresh instance starts
- **THEN** the master tenant exists with exactly the Superadmin and Member built-in roles and no members

#### Scenario: Master is protected
- **WHEN** the master tenant is targeted for suspension, or a caller without superadmin authority attempts to modify it
- **THEN** the request is rejected; superadmins MAY rename and change the master tenant's timezone via the admin surface

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
With `admin.users.read`/`admin.users.write`, a superadmin SHALL list users, create users, and disable/enable users globally. The users listing SHALL include, per user, whether that user holds the Superadmin role in the master tenant (`is_superadmin`), so one listing answers both identity and superadmin questions.

#### Scenario: Global disable
- **WHEN** a superadmin disables a user
- **THEN** the user is rejected at login and all authenticated requests return 401 across every workspace

#### Scenario: Listing flags superadmins
- **WHEN** a superadmin lists users
- **THEN** each item includes `is_superadmin`, true exactly for users holding a Superadmin-role membership in the master tenant

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

### Requirement: Tenant editing by superadmin
With `admin.workspaces.write`, a superadmin SHALL rename any workspace — including the master tenant — and change its timezone after creation, with creation-grade validation. The reserved slug SHALL never change; suspension and ownership transfer of the master tenant SHALL remain refused.

#### Scenario: Rename and re-zone
- **WHEN** a superadmin renames workspace "acme" to "Acme Corporation" and sets its timezone to "Asia/Jakarta"
- **THEN** the change persists and subsequent listings return the new name and timezone

#### Scenario: Master edited by superadmin
- **WHEN** a superadmin renames the master tenant and changes its timezone via the admin surface
- **THEN** the change persists; only superadmin authority can modify the master tenant, and its slug stays reserved

#### Scenario: Invalid edit values
- **WHEN** an edit supplies an empty name or an unknown timezone identifier
- **THEN** response is 400 invalid_request with a field-scoped error

### Requirement: Owner transfer by superadmin
With `admin.workspaces.write`, a superadmin SHALL change a workspace's owner to any user. The transfer SHALL be atomic: the target user becomes the workspace's Owner, every other owner-role holder is demoted to Admin, and a target who is not yet a member is added within the same transaction. After the transfer the workspace SHALL have exactly one owner. The master tenant SHALL reject ownership transfer here — its ownership is governed by the superadmin promotion flow.

#### Scenario: Transfer to a member
- **WHEN** ownership of "acme" is transferred to an existing Admin of "acme"
- **THEN** that Admin becomes the only Owner and the previous owner is demoted to Admin, in one operation

#### Scenario: Transfer to an outsider
- **WHEN** ownership is transferred to a user who is not a member of "acme"
- **THEN** the user is added to "acme" as Owner in the same operation and the previous owner is demoted to Admin

#### Scenario: Exactly one owner afterwards
- **WHEN** "acme" holds two owners and a transfer targets one of them
- **THEN** after the transfer exactly one owner remains

#### Scenario: Master transfer refused
- **WHEN** ownership transfer is attempted on the master tenant
- **THEN** response is 400 invalid_request and nothing changes

### Requirement: Tenant member management by superadmin
With `admin.workspaces.read`/`admin.workspaces.write`, a superadmin SHALL list the members of any workspace and add an existing user to any workspace with the Admin or Member role, regardless of the superadmin's own membership in that workspace. The Owner role SHALL NOT be assignable through tenant member management — ownership changes flow only through owner transfer. Adding an already-member user SHALL conflict; targeting an unknown user SHALL 404.

#### Scenario: List an outside tenant's members
- **WHEN** a superadmin lists the members of a workspace they hold no membership in
- **THEN** all members are returned with emails, names, and roles

#### Scenario: Add an Admin to an outside tenant
- **WHEN** a superadmin adds an existing user to "acme" with the Admin role
- **THEN** the membership is created and the user can act as an Admin of "acme"

#### Scenario: Owner role not assignable
- **WHEN** a superadmin attempts to add a member with the Owner role via tenant member management
- **THEN** response is 400 invalid_request

#### Scenario: Already a member
- **WHEN** the target user is already a member of the workspace
- **THEN** response is 409 conflict

#### Scenario: Unknown user
- **WHEN** the target user identifier does not match any account
- **THEN** response is 404 not found

### Requirement: OAuth app registration

Instance admins SHALL be able to register one OAuth app per provider: client id and client secret, stored encrypted with the instance-scoped key derivation and never returned in full after save (the secret is write-only; presence and last-4 hint only). The system SHALL display the exact redirect URI to configure at the provider, derived from the instance's public base URL, and SHALL report per-provider registration status that the connections gallery consumes to flip OAuth recipe cards to available.

#### Scenario: Register the Atlassian app

- **WHEN** an instance admin saves a client id and client secret for provider `atlassian`
- **THEN** the credentials are stored encrypted, the redirect URI is displayed for configuration at the provider, and Atlassian OAuth recipes report as available to workspaces

#### Scenario: Secret is write-only

- **WHEN** any client reads the OAuth app registration afterwards
- **THEN** the client secret is absent, with a last-4 hint only
