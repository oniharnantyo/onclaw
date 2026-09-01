## MODIFIED Requirements

### Requirement: Workspace creation
Creating a workspace SHALL atomically create the workspace, its three built-in roles, and the creator's Owner membership in one transaction. Creation SHALL validate the slug (DNS-label format, not reserved — `master` is instance-reserved) and reject duplicates with 409. The create payload MAY additionally include a `provider` object and a `starter_agent` object; when present, the provider config and starter agent SHALL be created in the same transaction (atomic birth): the starter agent is born configured (its provider_id points at the just-created config) with `prompts_status` `generating`, and its identity/soul prompts generate in the background per the agent-prompts capability. Provider and starter agent are all-or-nothing with the workspace: any validation failure aborts the entire birth.

#### Scenario: API creation
- **WHEN** an authenticated user POSTs {name, slug, timezone} to /workspaces
- **THEN** response is 201 with the workspace and the creator's Owner membership

#### Scenario: Birth with provider and starter agent
- **WHEN** the payload includes provider {type, name, base_url?, key} and starter_agent {name, slug, role, brief, model, ...}
- **THEN** 201 returns workspace + provider config + starter agent (prompts_status generating); all three exist or none do

#### Scenario: Birth validation failure aborts everything
- **WHEN** the starter agent payload fails validation (e.g. anthropic provider without max_tokens)
- **THEN** response is 400 and the workspace is NOT created

#### Scenario: Duplicate slug
- **WHEN** a slug already in use is submitted
- **THEN** response is 409 conflict

#### Scenario: Invalid slug
- **WHEN** the slug fails DNS-label format or is reserved
- **THEN** response is 400 invalid_request
