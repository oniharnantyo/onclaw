# members-roles Specification

## Purpose

Workspace membership and roles: built-in roles with permission sets, permission-set algebra for member management guards, and the member lifecycle (add by email, change role, remove/leave).

## Requirements

### Requirement: Built-in roles
Every workspace SHALL be created with three immutable built-in roles: Owner (all permissions + owner authority), Admin (all but roles.write), Member (reads only). Built-in roles SHALL NOT be editable or deletable.

#### Scenario: Seeded on creation
- **WHEN** a workspace is created
- **THEN** exactly three built-in roles exist: Owner/Admin/Member

#### Scenario: Not editable
- **WHEN** any attempt to edit or delete a built-in role
- **THEN** rejected (no role-write endpoint exists in this change)

### Requirement: Permission catalog
Access SHALL be governed by permission strings from a closed catalog (workspace.read/write, members.read/write/remove, roles.read/write, providers.read/write, agents.read/write, skills.read/write, integrations.write). Read access SHALL be a permission, not implied by membership. `agents.read` and `skills.read` SHALL join the read family granted to every built-in role; `agents.write` and `skills.write` SHALL be granted to Owner and Admin (and Superadmin, which holds all workspace permissions). `integrations.write` — service-connection management — SHALL be granted to the built-in Owner and Admin roles (and Superadmin via its all-workspace-permissions set), SHALL never be granted to Member, SHALL reach custom roles only when explicitly granted, and SHALL be backfilled into the built-in roles of workspaces created before the permission existed.

#### Scenario: Member role reads only
- **WHEN** a Member-role holder requests a write endpoint
- **THEN** response is 403 (their role's permission set lacks the write permission)

#### Scenario: Providers joins the read family
- **WHEN** a Member-role holder lists providers
- **THEN** 200 (providers.read is granted to every built-in role, like workspace.read/members.read/roles.read)

#### Scenario: Agents and skills join the read family
- **WHEN** a Member-role holder lists agents and lists skills
- **THEN** both return 200 (agents.read and skills.read are granted to every built-in role)

#### Scenario: Integrations management is admin-gated
- **WHEN** a Member-role holder attempts to connect, probe, or disconnect a service connection
- **THEN** response is 403 (integrations.write is held by built-in Owner and Admin, plus Superadmin)

#### Scenario: Custom role needs explicit grant
- **WHEN** a custom role holding the tool-settings write permission but not integrations.write attempts to connect a service connection
- **THEN** response is 403 (integrations.write reaches custom roles only by explicit grant)

#### Scenario: Backfilled into existing workspaces
- **WHEN** the migration runs on a workspace created before integrations.write existed
- **THEN** its built-in Owner and Admin roles — and the master tenant's Superadmin role — include integrations.write

### Requirement: Permission-set guards
Member management SHALL enforce the permission-set algebra: edit requires the target's permission set to be a strict subset of the actor's; assigning a role requires its set to be a subset of the actor's; equal sets (peers) SHALL NOT be manageable.

#### Scenario: Admin edits member
- **WHEN** an Admin manages a Member (strict subset)
- **THEN** allowed

#### Scenario: Peers not manageable
- **WHEN** an Admin attempts to manage another Admin (equal sets)
- **THEN** response is 403

#### Scenario: Member cannot manage anyone
- **WHEN** a Member attempts member management
- **THEN** response is 403 (lacks members.write/remove)

### Requirement: Member add
Adding a member SHALL be by email with a role assignment subject to canAssign. An unknown email SHALL auto-create the account (no password → cannot login until password-set flow). Already a member SHALL be 409.

#### Scenario: Add existing user
- **WHEN** admin adds an existing account's email
- **THEN** 201 with the membership

#### Scenario: Create on add
- **WHEN** an unknown email is added
- **THEN** account created (no password) and membership created; 201

#### Scenario: Already member
- **WHEN** the email is already a member of this workspace
- **THEN** response is 409 conflict

### Requirement: Role change / remove / leave
Role changes and removals SHALL respect the guards. Removing the last owner-role holder SHALL be refused with 409 last_owner_protected. A non-owner may leave by removing themselves.

#### Scenario: Role change guarded
- **WHEN** a Member-role holder tries to change anyone's role
- **THEN** response is 403

#### Scenario: Last-owner demotion refused
- **WHEN** an Owner demotes/removes the only owner-role holder (themselves or not)
- **THEN** 409 last_owner_protected

#### Scenario: Leave workspace
- **WHEN** a non-owner removes themselves
- **THEN** 204; workspace access ends

### Requirement: Roles listing
GET roles SHALL require roles.read and SHALL list the workspace's roles with id, name, permissions, built_in flag.

#### Scenario: List roles
- **WHEN** any member lists roles
- **THEN** all roles with permissions visible to members.read holders

### Requirement: Member listing
GET members SHALL require members.read and SHALL return user id, email, name, avatar URL, role, joined_at per member.

#### Scenario: List members
- **WHEN** any member lists members
- **THEN** fields present, avatar URL derived at read time from the storage key

### Requirement: Bootstrap owner
The bootstrap command SHALL make the created user Owner via the built-in Owner role.

#### Scenario: Bootstrap ownership
- **WHEN** bootstrap completes
- **THEN** the created user's membership uses the built-in Owner role
