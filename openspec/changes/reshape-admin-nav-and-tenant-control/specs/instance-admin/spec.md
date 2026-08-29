## MODIFIED Requirements

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

### Requirement: User management by superadmin
With `admin.users.read`/`admin.users.write`, a superadmin SHALL list users, create users, and disable/enable users globally. The users listing SHALL include, per user, whether that user holds the Superadmin role in the master tenant (`is_superadmin`), so one listing answers both identity and superadmin questions.

#### Scenario: Global disable
- **WHEN** a superadmin disables a user
- **THEN** the user is rejected at login and all authenticated requests return 401 across every workspace

#### Scenario: Listing flags superadmins
- **WHEN** a superadmin lists users
- **THEN** each item includes `is_superadmin`, true exactly for users holding a Superadmin-role membership in the master tenant

## ADDED Requirements

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
