# agents Delta

## MODIFIED Requirements

### Requirement: Agent CRUD
Tenant-scoped endpoints under `/workspaces/:ws/agents` SHALL create, list, get, update, and delete agents. Routes SHALL address agents by slug; the store port SHALL also resolve by ID internally. Requests require `agents.read` for list/get and `agents.write` for create/update/delete. Non-members and unknown slugs SHALL receive 404 indistinguishably. Listing SHALL return the roster with name, slug, role, description, model, provider, autonomy, avatar, and `prompts_status`. Listing SHALL return agents ordered by creation time, newest first, with the agent id descending as tiebreaker. List responses SHALL NOT include generated prompt documents (identity, soul, bootstrap); clients SHALL read those from the agent detail endpoint.

#### Scenario: Create with wizard defaults
- **WHEN** an Owner POSTs {name, slug, role, description, brief, provider_id, model} with no capabilities
- **THEN** response is 201; the agent exists with an empty `tools` array (no registry tools enabled) and an empty `enabled_mcps` array (no workspace MCP servers enabled — see the workspace-mcp capability), autonomy `approval`, temperature 1.0, `context_window` auto-filled per the Agent fields requirement, and `prompts_status` `ready` (generation gates persistence — see agent-prompts)

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
An agent SHALL have: name, slug (DNS-label rules, reserved list shared with workspaces, unique per workspace, immutable after creation), role (short free-form, kebab-case convention with UI suggestions), description (display one-liner), brief (generation driver, required at create), provider and model (required, free-text model id), autonomy (`approval|suggest|full`, default `approval`), temperature (0–2, default 1.0), max_tokens (optional positive integer, required for `anthropic` provider type), effort (optional provider-neutral string), avatar (JSON object of react-nice-avatar props, loosely validated — must be a JSON object under a size cap), `context_window` (optional positive integer token count), `tools` (the agent's tool allowlist: registry tool names plus the reserved shell name `execute`; an empty array enables no registry tools), and `enabled_mcps` (the agent's opt-in allowlist of workspace MCP server ids; an empty array enables no workspace MCP servers — see the workspace-mcp capability). The slug SHALL be immutable: a slug in an update payload SHALL be ignored (managed-field semantics, like `prompts_status`). `identity`, `soul`, and `bootstrap` SHALL be server-managed **file-backed** fields: the documents live as `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` in the agent's workspace directory (see the agent-prompts capability), are composed onto the agent on every read, and are absent from the database. `identity` and `soul` become editable via update once generated — the update writes the files; `bootstrap` is not client-editable. `memory`, `prompts_status`, and `prompts_error` SHALL be ignored on any client input.

`context_window` SHALL resolve at creation/update when the client omits it: the server SHALL auto-fill it from the model catalog's context limit for the provider/model when the catalog knows the model, and SHALL leave it unset (runtime falls back to its default) otherwise. A client-provided value always wins and SHALL be stored as given. `context_window` MUST be a positive integer when present; zero or negative SHALL be 400 invalid_request. `tools` replaces the former `disabled_tools` denylist, which no longer exists in the schema, API, or runtime. The `tools` allowlist and the `enabled_mcps` allowlist SHALL NOT be referentially validated against stored rows — names and ids resolve against runtime registries at execution time, so unknown values are inert, not errors. `enabled_mcps` supersedes the former `disabled_mcps` denylist, which SHALL NOT exist in the schema or API: a `disabled_mcps` value in any payload SHALL be ignored like a managed field. The former `skills` allowlist and `disabled_skills` denylist fields SHALL NOT exist in the schema or API: skills are governed at tier level (system always attached; workspace skills by the registry master switch; agent-tier by presence — see the workspace-skills capability), and a `disabled_skills` value in any payload SHALL be ignored like a managed field.

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

#### Scenario: Context window auto-filled from catalog
- **WHEN** an agent is created without `context_window` on a provider/model the catalog knows (e.g. openai gpt-5)
- **THEN** the stored agent carries the catalog's context limit for that model

#### Scenario: Context window override wins
- **WHEN** a create or update sets `context_window` explicitly
- **THEN** the provided value is stored and catalog auto-fill does not run

#### Scenario: Context window must be positive
- **WHEN** a create or update sets `context_window` to 0 or a negative number
- **THEN** response is 400 invalid_request

#### Scenario: Tools patch replaces the allowlist
- **WHEN** an update payload includes `tools`
- **THEN** the stored allowlist is replaced by the payload value (including the empty array, which enables no registry tools)

#### Scenario: Enabled MCPs patch replaces the allowlist
- **WHEN** an update payload includes `enabled_mcps`
- **THEN** the stored allowlist is replaced by the payload value (including the empty array, which enables no workspace MCP servers)

#### Scenario: Unknown names are not referentially validated
- **WHEN** an agent is saved with `tools` or `enabled_mcps` naming capabilities that exist in no registry or directory
- **THEN** the save succeeds; the unknown values are inert at execution time

#### Scenario: Legacy denylist field ignored
- **WHEN** a create or update payload includes `disabled_mcps`
- **THEN** the value is accepted without error but ignored (no stored state); MCP exposure is governed solely by `enabled_mcps`

#### Scenario: Disabled skill accepted
- **WHEN** a create or update payload includes `disabled_skills`
- **THEN** the value is accepted without error but ignored (no stored state); skill attachment is governed solely by tier rules
