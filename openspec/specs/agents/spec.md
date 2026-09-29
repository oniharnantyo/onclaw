# agents Specification

## Purpose
Agents are the core tenant entity: named, slug-addressed personas bound to a workspace provider config and model, created through a three-step wizard and manageable via CRUD endpoints scoped to the workspace.

## Requirements

### Requirement: Agent CRUD
Tenant-scoped endpoints under `/workspaces/:ws/agents` SHALL create, list, get, update, and delete agents. Routes SHALL address agents by slug; the store port SHALL also resolve by ID internally. Requests require `agents.read` for list/get and `agents.write` for create/update/delete. Non-members and unknown slugs SHALL receive 404 indistinguishably. Listing SHALL return the roster with name, slug, role, description, model, provider, autonomy, avatar, and `prompts_status`. Listing SHALL return agents ordered by creation time, newest first, with the agent id descending as tiebreaker. List responses SHALL NOT include generated prompt documents (identity, soul, bootstrap); clients SHALL read those from the agent detail endpoint.

#### Scenario: Create with wizard defaults
- **WHEN** an Owner POSTs {name, slug, role, description, brief, provider_id, model} with no capabilities
- **THEN** response is 201; the agent exists with an empty `disabled_tools` array (every catalog tool enabled) and an empty `enabled_mcps` array (no workspace MCP servers enabled — see the workspace-mcp capability), autonomy `approval`, temperature 1.0, `context_window` auto-filled per the Agent fields requirement, and `prompts_status` `ready` (generation gates persistence — see agent-prompts)

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
An agent SHALL have: name, slug (DNS-label rules, reserved list shared with workspaces, unique per workspace, immutable after creation), role (short free-form, kebab-case convention with UI suggestions), description (display one-liner), brief (generation driver, required at create), provider and model (a both-set pair pinning the agent — model a free-text id — or both empty meaning inherit the workspace default, see Provider binding), autonomy (`approval|suggest|full`, default `approval`), temperature (0–2, default 1.0), max_tokens (optional positive integer; at save required when the pinned provider's type requires it — enforced at run start for inheriting agents), effort (optional provider-neutral string), avatar (JSON object of react-nice-avatar props, loosely validated — must be a JSON object under a size cap), `context_window` (optional positive integer token count), `disabled_tools` (the agent's tool denylist: catalog tool keys — registry built-ins, filesystem middleware tool names, the reserved shell name `execute`, the `browser` facade alias, and reserved capability names; an empty array disables nothing, exposing every catalog tool subject to the workspace gate), and `enabled_mcps` (the agent's opt-in allowlist of workspace MCP server ids; an empty array enables no workspace MCP servers — see the workspace-mcp capability). The slug SHALL be immutable: a slug in an update payload SHALL be ignored (managed-field semantics, like `prompts_status`). `identity` and `soul` SHALL be server-managed **file-backed** fields: the documents live as `IDENTITY.md` and `SOUL.md` in the agent's workspace directory (see the agent-prompts capability), are composed onto the agent on every read, and are absent from the database. `identity` and `soul` become editable via update once generated — the update writes the files. `memory`, `prompts_status`, and `prompts_error` SHALL be ignored on any client input.

`context_window` SHALL resolve at creation/update when the client omits it: the server SHALL auto-fill it from the model catalog's context limit for the provider/model when the catalog knows the model, and SHALL leave it unset (runtime falls back to its default) otherwise — for an inheriting agent the pair is unknown at save, so the catalog auto-fill cannot resolve and `context_window` stays unset unless client-provided, with runtime resolution applying to the effective model at run start. A client-provided value always wins and SHALL be stored as given. `context_window` MUST be a positive integer when present; zero or negative SHALL be 400 invalid_request. `disabled_tools` reinstates the denylist that the former `tools` allowlist replaced; the `tools` field no longer exists in the schema, API, or runtime, and a `tools` value in any payload SHALL be ignored like a managed field. The `disabled_tools` denylist and the `enabled_mcps` allowlist SHALL NOT be referentially validated against stored rows — names and ids resolve against runtime registries at execution time, so unknown values are inert, not errors. `enabled_mcps` supersedes the former `disabled_mcps` denylist, which SHALL NOT exist in the schema or API: a `disabled_mcps` value in any payload SHALL be ignored like a managed field. The former `skills` allowlist and `disabled_skills` denylist fields SHALL NOT exist in the schema or API: skills are governed at tier level (system always attached; workspace skills by the registry master switch; agent-tier by presence — see the workspace-skills capability), and a `disabled_skills` value in any payload SHALL be ignored like a managed field.

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
- **WHEN** a client fetches an agent or a create/update payload includes `bootstrap`
- **THEN** the field is absent from the response and ignored on input; no bootstrap document is seeded or projected

#### Scenario: Anthropic requires max_tokens
- **WHEN** an agent is created or updated pinned to an `anthropic`-type provider with no max_tokens
- **THEN** response is 400 invalid_request

#### Scenario: Inheriting agent skips save-time max_tokens check
- **WHEN** an agent is saved with the empty pair while its effective provider type at run start requires max_tokens
- **THEN** the save succeeds and the requirement is enforced by the runtime (see the agent-runtime capability)

#### Scenario: Effort validated against resolution
- **WHEN** an agent is saved with a non-empty effort
- **THEN** it SHALL be validated against the resolved effort values for that provider/model (catalog values union static floor); an unmatched value is 400 invalid_request — for an inheriting agent the validation is deferred to run start against the effective provider/model

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
- **WHEN** an update payload includes `disabled_tools`
- **THEN** the stored denylist is replaced by the payload value (including the empty array, which disables nothing)

#### Scenario: Enabled MCPs patch replaces the allowlist
- **WHEN** an update payload includes `enabled_mcps`
- **THEN** the stored allowlist is replaced by the payload value (including the empty array, which enables no workspace MCP servers)

#### Scenario: Unknown names are not referentially validated
- **WHEN** an agent is saved with `disabled_tools` or `enabled_mcps` naming capabilities that exist in no registry or directory
- **THEN** the save succeeds; the unknown values are inert at execution time

#### Scenario: Legacy allowlist field ignored
- **WHEN** a create or update payload includes `tools`
- **THEN** the value is accepted without error but ignored (no stored state); tool exposure is governed solely by `disabled_tools`

#### Scenario: Legacy denylist field ignored
- **WHEN** a create or update payload includes `disabled_mcps`
- **THEN** the value is accepted without error but ignored (no stored state); MCP exposure is governed solely by `enabled_mcps`

#### Scenario: Disabled skill accepted
- **WHEN** a create or update payload includes `disabled_skills`
- **THEN** the value is accepted without error but ignored (no stored state); skill attachment is governed solely by tier rules

### Requirement: Provider binding
An agent's provider config SHALL be a both-set-or-both-empty pair: a pinned agent carries `provider_id` + `model` (model a free-text id); an inheriting agent carries both empty and SHALL resolve to the workspace default model at run start (see the agent-runtime capability). Creating or updating an agent with the empty pair SHALL be refused with 422 while the workspace has no default model. For pinned agents the binding SHALL be guaranteed to belong to the same workspace at the data layer: the composite foreign key `(workspace_id, provider_id) → workspace_providers(workspace_id, id)` SHALL make cross-tenant provider references impossible in the schema (empty/NULL provider columns are outside the constraint and mean inheritance, not a binding).

#### Scenario: Cross-workspace provider rejected
- **WHEN** a create or update references a provider id from another workspace
- **THEN** response is 400 invalid_request (mapped from the data-layer violation; enumeration defense via 404 pre-check)

#### Scenario: Inherit agent stored with empty pair
- **WHEN** a create or update payload omits provider_id and model (both empty) while the workspace has a default model
- **THEN** the agent persists with both fields empty and runs on the workspace default

#### Scenario: Inherit refused without a default
- **WHEN** a create or update payload carries the empty pair while the workspace has no default model
- **THEN** response is 422 naming the missing workspace setting

### Requirement: Deletion semantics
Deleting an agent SHALL cascade to its per-user memories and SHALL remove the agent's workspace directory from disk. Deleting a provider config that agents reference SHALL be refused (see providers capability). Deleting an agent SHALL NOT affect providers, skills, or other agents.

#### Scenario: Delete agent with memories
- **WHEN** an agent with per-user memories is deleted
- **THEN** 204; the memories are gone; the workspace's skills and providers are untouched

#### Scenario: Delete removes the workspace directory
- **WHEN** an agent is deleted
- **THEN** its workspace directory and the prompt documents inside it are removed from disk

### Requirement: Derived agent workspace directory
Every agent SHALL have an on-disk workspace directory at `<ONCLAW_DIR>/workspaces/<tenant_slug>/agents/<agent_slug>`, derived at the point of use from the current root and slugs. All server file locations SHALL derive from the single root `ONCLAW_DIR` (environment variable, defaulting to `$HOME/.onclaw`; the former `ONCLAW_WORKSPACE_DIR` knob no longer exists — the workspace root is `<ONCLAW_DIR>/workspaces`). The server SHALL refuse to start when `ONCLAW_DIR` is not an absolute path. The path SHALL NOT be recorded in the database; the agent row carries no `workspace_dir`. The directory SHALL be created before the agent row is written — in both the create endpoint and the atomic birth — so a directory-creation failure rejects the request before any DB write. Because slugs are immutable, the derived path is stable for the agent's lifetime.

#### Scenario: Directory created at deploy
- **WHEN** an agent is created
- **THEN** `<ONCLAW_DIR>/workspaces/<tenant_slug>/agents/<agent_slug>` exists on disk; the agent response carries no `workspace_dir` field

#### Scenario: Starter agent directory at birth
- **WHEN** a workspace is born with a starter agent
- **THEN** the starter agent's directory exists before the birth transaction commits

#### Scenario: Relative workspace root rejected at startup
- **WHEN** the server starts with a relative `ONCLAW_DIR`
- **THEN** startup fails with a configuration error instead of deriving cwd-dependent paths

### Requirement: Effective context exposure
The agents API SHALL expose, on the agent payload, two computed read-only fields describing the context budget execution actually enforces: `effective_context_window` — the agent's resolved context window (its stored `context_window`, or the 200,000-token default when none is stored) — and `summarization_trigger_tokens` — the effective window multiplied by the server's summarization margin. The values SHALL be derived at read time from the same resolution execution applies, and clients SHALL treat them as authoritative display data rather than recomputing resolution or duplicating the margin constant. These fields SHALL NOT be writable through create or update requests.

#### Scenario: Stored window is echoed as effective
- **WHEN** an agent has a stored `context_window` of 50,000
- **THEN** its payload reports `effective_context_window` 50,000 and `summarization_trigger_tokens` 37,500 with a 0.75 server margin

#### Scenario: Unset window falls back to the default
- **WHEN** an agent has no stored `context_window`
- **THEN** its payload reports `effective_context_window` 200,000

#### Scenario: Not writable
- **WHEN** a create or update request supplies `effective_context_window` or `summarization_trigger_tokens`
- **THEN** the request ignores those fields and the stored agent is unaffected
