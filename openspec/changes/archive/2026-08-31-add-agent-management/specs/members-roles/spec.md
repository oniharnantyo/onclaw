## MODIFIED Requirements

### Requirement: Permission catalog
Access SHALL be governed by permission strings from a closed catalog (workspace.read/write, members.read/write/remove, roles.read/write, providers.read/write, agents.read/write, skills.read/write). Read access SHALL be a permission, not implied by membership. `agents.read` and `skills.read` SHALL join the read family granted to every built-in role; `agents.write` and `skills.write` SHALL be granted to Owner and Admin (and Superadmin, which holds all workspace permissions).

#### Scenario: Member role reads only
- **WHEN** a Member-role holder requests a write endpoint
- **THEN** response is 403 (their role's permission set lacks the write permission)

#### Scenario: Providers joins the read family
- **WHEN** a Member-role holder lists providers
- **THEN** 200 (providers.read is granted to every built-in role, like workspace.read/members.read/roles.read)

#### Scenario: Agents and skills join the read family
- **WHEN** a Member-role holder lists agents and lists skills
- **THEN** both return 200 (agents.read and skills.read are granted to every built-in role)
