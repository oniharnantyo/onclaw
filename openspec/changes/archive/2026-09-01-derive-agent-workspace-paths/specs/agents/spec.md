# agents (delta)

## REMOVED Requirements

### Requirement: Agent workspace directory
**Reason:** the recorded-path design is replaced wholesale by derived paths; see the ADDED requirement "Derived agent workspace directory".

## MODIFIED Requirements

### Requirement: Agent CRUD
Tenant-scoped endpoints under `/workspaces/:ws/agents` SHALL create, list, get, update, and delete agents. Routes SHALL address agents by slug; the store port SHALL also resolve by ID internally. Requests require `agents.read` for list/get and `agents.write` for create/update/delete. Non-members and unknown slugs SHALL receive 404 indistinguishably. Listing SHALL return the roster with name, slug, role, description, model, provider, autonomy, avatar, and `prompts_status`. Listing SHALL return agents ordered by creation time, newest first, with the agent id descending as tiebreaker. List responses SHALL NOT include generated prompt documents (identity, soul, bootstrap); clients SHALL read those from the agent detail endpoint.

#### Scenario: Create with wizard defaults
- **WHEN** an Owner POSTs {name, slug, role, description, brief, provider_id, model} with no capabilities
- **THEN** response is 201; the agent exists with empty tools/skills/mcp arrays, autonomy `approval`, temperature 1.0, and `prompts_status` `ready` (generation gates persistence — see agent-prompts)

#### Scenario: Slug conflict
- **WHEN** a create submits a slug already used in this workspace
- **THEN** response is 409 conflict

#### Scenario: Invalid slug
- **WHEN** a slug fails DNS-label validation or hits the reserved list
- **THEN** response is 400 invalid_request

#### Scenario: Cross-tenant agent is not found
- **WHEN** a member of workspace A requests an agent addressed by slug belonging to workspace B
- **THEN** response is 404, indistinguishable from an unknown slug

#### Scenario: Member reads the roster
- **WHEN** a Member-role holder lists agents
- **THEN** response is 200 with the roster (agents.read is granted to every built-in role)

#### Scenario: Member cannot create
- **WHEN** a Member-role holder POSTs an agent
- **THEN** response is 403

#### Scenario: Roster order is newest first
- **WHEN** agents "A", "B", "C" are created in that order in one workspace
- **THEN** listing returns them C, B, A (created_at DESC, id DESC)

#### Scenario: List omits prompt documents
- **WHEN** the roster is listed
- **THEN** entries do not carry identity/soul/bootstrap document content; the detail endpoint returns them

### Requirement: Agent fields
An agent SHALL have: name, slug (DNS-label rules, reserved list shared with workspaces, unique per workspace, immutable after creation), role (short free-form, kebab-case convention with UI suggestions), description (display one-liner), brief (generation driver, required at create), provider and model (required, free-text model id), autonomy (`approval|suggest|full`, default `approval`), temperature (0–2, default 1.0), max_tokens (optional positive integer, required for `anthropic` provider type), effort (optional provider-neutral string), avatar (JSON object of react-nice-avatar props, loosely validated — must be a JSON object under a size cap), and skills (array validated against the workspace's `workspace_skills` names, disabled ones included; the runtime skips disabled skills). The slug SHALL be immutable: a slug in an update payload SHALL be ignored (managed-field semantics, like `prompts_status`). `identity`, `soul`, and `bootstrap` SHALL be server-managed **file-backed** fields: the documents live as `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` in the agent's workspace directory (see the agent-prompts capability), are composed onto the agent on every read, and are absent from the database. `identity` and `soul` become editable via update once generated — the update writes the files; `bootstrap` is not client-editable. `memory`, `prompts_status`, and `prompts_error` SHALL be ignored on any client input.

#### Scenario: Slug is immutable
- **WHEN** an update payload includes a slug (changed or unchanged)
- **THEN** the value is ignored; the agent's slug is unchanged

#### Scenario: Managed fields ignored
- **WHEN** a create or update payload includes memory, prompts_status, or prompts_error
- **THEN** the values are ignored; server state is unchanged

#### Scenario: Prompts editable after generation
- **WHEN** a user updates identity or soul after prompts_status is ready
- **THEN** `IDENTITY.md` and `SOUL.md` are rewritten in the agent's workspace directory and the status is unaffected

#### Scenario: Bootstrap is not client-editable
- **WHEN** a create or update payload includes bootstrap
- **THEN** the value is ignored; `BOOTSTRAP.md` is written only by the generator

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

## ADDED Requirements

### Requirement: Derived agent workspace directory
Every agent SHALL have an on-disk workspace directory at `<workspace_dir_root>/<tenant_slug>/agents/<agent_slug>`, derived at the point of use from the current root and slugs. The root SHALL come from `ONCLAW_WORKSPACE_DIR` (e.g. `/var/lib/onclaw/.onclaw/workspaces`), defaulting to `$HOME/.onclaw/workspaces` when unset. The server SHALL refuse to start when the configured root is not an absolute path. The path SHALL NOT be recorded in the database; the agent row carries no `workspace_dir`. The directory SHALL be created before the agent row is written — in both the create endpoint and the atomic birth — so a directory-creation failure rejects the request before any DB write. Because slugs are immutable, the derived path is stable for the agent's lifetime.

#### Scenario: Directory created at deploy
- **WHEN** an agent is created
- **THEN** `<root>/<tenant_slug>/agents/<agent_slug>` exists on disk; the agent response carries no `workspace_dir` field

#### Scenario: Starter agent directory at birth
- **WHEN** a workspace is born with a starter agent
- **THEN** the starter agent's directory exists before the birth transaction commits

#### Scenario: Relative workspace root rejected at startup
- **WHEN** the server starts with a relative `ONCLAW_WORKSPACE_DIR` (from env or `--workspace-dir`)
- **THEN** startup fails with a configuration error instead of deriving cwd-dependent paths
