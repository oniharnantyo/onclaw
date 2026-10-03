## MODIFIED Requirements

### Requirement: Built-in roles
Every workspace SHALL be created with three immutable built-in roles: Owner (all permissions + owner authority), Admin (all but the retired roles.write — i.e. every remaining catalog permission), Member (the read family plus channel participation). Built-in roles SHALL NOT be editable or deletable.

#### Scenario: Seeded on creation
- **WHEN** a workspace is created
- **THEN** exactly three built-in roles exist: Owner/Admin/Member

#### Scenario: Not editable
- **WHEN** any attempt to edit or delete a built-in role
- **THEN** rejected (no role-write endpoint exists in this change)

### Requirement: Permission catalog
Access SHALL be governed by permission strings from a closed catalog (workspace.read/write, members.read/write/remove, roles.read, providers.read/write, agents.read/write, skills.read/write, tools.write, hooks.read/write, channels.read/write, scheduler.read/write, gateways.write, integrations.write, reference_documents.promote, and the admin.* instance permissions). The former `roles.write` permission SHALL NOT exist in the catalog, SHALL NOT appear in any role's permission set, and SHALL be stripped from existing role rows by migration. `channels.read` and `channels.write` SHALL be granted to every built-in role including Member, so members can list channels, post messages, and manage channel membership. Read access SHALL be a permission, not implied by membership. `agents.read` and `skills.read` SHALL join the read family granted to every built-in role; `agents.write` and `skills.write` SHALL be granted to Owner and Admin (and Superadmin, which holds all workspace permissions). `integrations.write` — service-connection management — SHALL be granted to the built-in Owner and Admin roles (and Superadmin via its all-workspace-permissions set), SHALL never be granted to Member, SHALL reach custom roles only when explicitly granted, and SHALL be backfilled into the built-in roles of workspaces created before the permission existed.

#### Scenario: Member role reads only
- **WHEN** a Member-role holder requests a workspace-administration write endpoint (agent, provider, skill, tool, scheduler, gateway, integration, hook, member, or workspace settings mutation)
- **THEN** response is 403 (their role's permission set lacks the required write permission)

#### Scenario: Channels join the member grant
- **WHEN** a Member-role holder lists channels, posts a channel message, or adds a member to a channel
- **THEN** each succeeds (channels.read and channels.write are granted to every built-in role)

#### Scenario: roles.write is retired
- **WHEN** the migration runs on any workspace
- **THEN** no role row's permission set contains roles.write, and no endpoint accepts or requires it

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
