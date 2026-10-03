## MODIFIED Requirements

### Requirement: Workspace creation
Creating a workspace SHALL be restricted to instance administrators: the caller SHALL be a member of the master workspace holding `admin.workspaces.write`; any other authenticated caller SHALL be refused with 403. Creation SHALL atomically create the workspace, its three built-in roles, and the creator's Owner membership in one transaction. The create payload MAY include a `description` (free text, display description of the workspace; optional). Creation SHALL validate the slug (DNS-label format, not reserved — `master` is instance-reserved) and reject duplicates with 409. The create payload MAY additionally include a `provider` object and a `starter_agent` object; when present, the provider config and starter agent SHALL be created in the same transaction (atomic birth): the starter agent is born configured (its provider_id points at the just-created config) with `prompts_status` `generating`, and its identity/soul prompts generate in the background per the agent-prompts capability. Provider and starter agent are all-or-nothing with the workspace: any validation failure aborts the entire birth.

#### Scenario: API creation
- **WHEN** an instance administrator (master-workspace member holding admin.workspaces.write) POSTs {name, slug, timezone, description?} to /workspaces
- **THEN** response is 201 with the workspace (carrying the description when provided) and the creator's Owner membership

#### Scenario: Creation refused for non-admins
- **WHEN** an authenticated user who is not a master-workspace admin POSTs to /workspaces
- **THEN** response is 403 and no workspace, role, or membership row is created

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
