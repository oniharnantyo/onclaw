## Purpose

Agents are the core tenant entity: named, slug-addressed personas bound to a workspace provider config and model, created through a three-step wizard and manageable via CRUD endpoints scoped to the workspace.

## ADDED Requirements

### Requirement: Agent CRUD
Tenant-scoped endpoints under `/workspaces/:ws/agents` SHALL create, list, get, update, and delete agents. Routes SHALL address agents by slug; the store SHALL also resolve by ID internally. Requests require `agents.read` for list/get and `agents.write` for create/update/delete. Non-members and unknown slugs SHALL receive 404 indistinguishably. Listing SHALL return the roster with name, slug, role, description, model, provider, autonomy, avatar, and `prompts_status`.

#### Scenario: Create with wizard defaults
- **WHEN** an Owner POSTs {name, slug, role, description, brief, provider_id, model} with no capabilities
- **THEN** response is 201; the agent exists with empty tools/skills/mcp arrays, autonomy `approval`, temperature 1.0, and `prompts_status` `generating`

#### Scenario: Slug conflict
- **WHEN** a create or slug-changing PATCH submits a slug already used in this workspace
- **THEN** response is 409 conflict

#### Scenario: Invalid slug
- **WHEN** a slug fails DNS-label validation or hits the reserved list
- **THEN** response is 400 invalid_request

#### Scenario: Cross-tenant agent is not found
- **WHEN** a member of workspace A requests an agent addressed by slug belonging to workspace B
- **THEN** response is 404, indistinguishable from an unknown slug

#### Scenario: Member reads the roster
- **WHEN** a Member-role holder lists agents
- **THEN** 200 with the roster (agents.read is granted to every built-in role)

#### Scenario: Member cannot create
- **WHEN** a Member-role holder POSTs an agent
- **THEN** response is 403

### Requirement: Agent fields
An agent SHALL have: name, slug (DNS-label rules, reserved list shared with workspaces, unique per workspace), role (short free-form, kebab-case convention with UI suggestions), description (display one-liner), brief (generation driver, required at create), provider and model (required, free-text model id), autonomy (`approval|suggest|full`, default `approval`), temperature (0–2, default 1.0), max_tokens (optional positive integer, required for `anthropic` provider type), effort (optional provider-neutral string), avatar (JSON object of react-nice-avatar props, loosely validated — must be a JSON object under a size cap), and skills (array validated against the workspace's `workspace_skills` names, disabled ones included; the runtime skips disabled skills). `identity`, `soul`, `memory`, `prompts_status`, and `prompts_error` SHALL be server-managed: `identity` and `soul` become editable via update once generated, while `memory`, `prompts_status`, and `prompts_error` are ignored on any client input.

#### Scenario: Managed fields ignored
- **WHEN** a create or update payload includes memory, prompts_status, or prompts_error
- **THEN** the values are ignored; server state is unchanged

#### Scenario: Prompts editable after generation
- **WHEN** a user updates identity or soul after prompts_status is ready
- **THEN** the new text is stored and the status is unaffected

#### Scenario: Anthropic requires max_tokens
- **WHEN** an agent is created or updated with an `anthropic`-type provider and no max_tokens
- **THEN** response is 400 invalid_request

#### Scenario: Effort validated against resolution
- **WHEN** an agent is saved with a non-empty effort
- **THEN** it SHALL be validated against the resolved effort values for that provider/model (catalog values union static floor); an unmatched value is 400 invalid_request

#### Scenario: Avatar must be a props object
- **WHEN** avatar is provided as a string, or as a JSON object exceeding 2 KB
- **THEN** response is 400 invalid_request

#### Scenario: Unknown skill name rejected
- **WHEN** an agent is saved referencing a skill name with no workspace_skills row in this workspace
- **THEN** response is 400 invalid_request

#### Scenario: Disabled skill accepted
- **WHEN** an agent references a disabled workspace skill
- **THEN** the save succeeds; the runtime skips disabled skills when the agent executes

### Requirement: Provider binding
An agent's provider config SHALL be required and SHALL be guaranteed to belong to the same workspace at the data layer: the composite foreign key `(workspace_id, provider_id) → workspace_providers(workspace_id, id)` SHALL make cross-tenant provider references impossible in the schema. `model` SHALL be a required free-text id.

#### Scenario: Cross-workspace provider rejected
- **WHEN** a create or update references a provider id from another workspace
- **THEN** response is 400 invalid_request (mapped from the data-layer violation; enumeration defense via 404 pre-check)

### Requirement: Deletion semantics
Deleting an agent SHALL cascade to its per-user memories. Deleting a provider config that agents reference SHALL be refused (see providers capability). Deleting an agent SHALL NOT affect providers, skills, or other agents.

#### Scenario: Delete agent with memories
- **WHEN** an agent with per-user memories is deleted
- **THEN** 204; the memories are gone; the workspace's skills and providers are untouched

### Requirement: Agent workspace directory
Every agent SHALL have an on-disk workspace directory at `<workspace_dir_root>/<tenant_slug>/agents/<agent_slug>`, recorded in the agent's `workspace_dir` column. The root SHALL come from `ONCLAW_WORKSPACE_DIR` (e.g. `/var/lib/onclaw/.onclaw/workspaces`), defaulting to `$HOME/.onclaw/workspaces` when unset; the stored `workspace_dir` is an absolute path. The directory SHALL be created before the agent row is written — in both the create endpoint and the atomic birth — so a directory-creation failure rejects the request before any DB write. The path is assigned at creation and SHALL remain stable across later slug renames.

#### Scenario: Directory created at deploy
- **WHEN** an agent is created
- **THEN** `<root>/<tenant_slug>/agents/<agent_slug>` exists on disk and the agent's `workspace_dir` holds that absolute path

#### Scenario: Starter agent directory at birth
- **WHEN** a workspace is born with a starter agent
- **THEN** the starter agent's directory exists and its `workspace_dir` is set before the birth transaction commits

#### Scenario: Rename keeps the directory stable
- **WHEN** an agent's slug is changed after creation
- **THEN** PATCH only changes the slug; `workspace_dir` and the on-disk directory are unchanged
