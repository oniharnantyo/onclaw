## MODIFIED Requirements

### Requirement: Permission catalog
Access SHALL be governed by permission strings from a closed catalog (workspace.read/write, members.read/write/remove, roles.read/write, providers.read/write). Read access SHALL be a permission, not implied by membership.

#### Scenario: Member role reads only
- **WHEN** a Member-role holder requests a write endpoint
- **THEN** response is 403 (their role's permission set lacks the write permission)

#### Scenario: Providers joins the read family
- **WHEN** a Member-role holder lists providers
- **THEN** 200 (providers.read is granted to every built-in role, like workspace.read/members.read/roles.read)
