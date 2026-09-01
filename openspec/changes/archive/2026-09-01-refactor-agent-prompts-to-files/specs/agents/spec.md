## MODIFIED Requirements

### Requirement: Agent fields
An agent SHALL have: name, slug (DNS-label rules, reserved list shared with workspaces, unique per workspace), role (short free-form, kebab-case convention with UI suggestions), description (display one-liner), brief (generation driver, required at create), provider and model (required, free-text model id), autonomy (`approval|suggest|full`, default `approval`), temperature (0–2, default 1.0), max_tokens (optional positive integer, required for `anthropic` provider type), effort (optional provider-neutral string), avatar (JSON object of react-nice-avatar props, loosely validated — must be a JSON object under a size cap), and skills (array validated against the workspace's `workspace_skills` names, disabled ones included; the runtime skips disabled skills). `identity`, `soul`, and `bootstrap` SHALL be server-managed **file-backed** fields: the documents live as `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` in the agent's workspace directory (see the agent-prompts capability), are composed onto the agent on every read, and are absent from the database. `identity` and `soul` become editable via update once generated — the update writes the files; `bootstrap` is not client-editable. `memory`, `prompts_status`, and `prompts_error` SHALL be ignored on any client input.

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

### Requirement: Deletion semantics
Deleting an agent SHALL cascade to its per-user memories and SHALL remove the agent's workspace directory from disk. Deleting a provider config that agents reference SHALL be refused (see providers capability). Deleting an agent SHALL NOT affect providers, skills, or other agents.

#### Scenario: Delete agent with memories
- **WHEN** an agent with per-user memories is deleted
- **THEN** 204; the memories are gone; the workspace's skills and providers are untouched

#### Scenario: Delete removes the workspace directory
- **WHEN** an agent is deleted
- **THEN** its workspace directory and the prompt documents inside it are removed from disk
