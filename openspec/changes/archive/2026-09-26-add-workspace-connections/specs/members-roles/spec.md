## MODIFIED Requirements

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
