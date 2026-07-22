## MODIFIED Requirements

### Requirement: Named agents are stored in an `agents` table and selected per run

The system SHALL store agent definitions in an `agents` table (name, provider, model, model metadata, reasoning control, description, workspace, tools, max iterations, memory configuration, enabled). `onclaw run` and `onclaw chat` SHALL select the agent to use from a `--agent <name>` flag, or when absent from a `default_agent` preference. The selected agent SHALL be the only agent that runs for that invocation. Running with no `--agent` and no `default_agent` SHALL fail with a clear error. The stored `description` is agent metadata (a free-text statement of what the agent is for); it SHALL NOT be folded into the agent's system prompt and SHALL be passed to the eino agent as its `Description` (cross-ref `agent-identity`).

#### Scenario: The default agent is used when `--agent` is absent

- **WHEN** a `default_agent` preference is set to `coder` and the user runs `onclaw run "hi"`
- **THEN** the `coder` agent runs

#### Scenario: `--agent` selects a named agent

- **WHEN** the user runs `onclaw run --agent reviewer "hi"`
- **THEN** the `reviewer` agent runs, regardless of `default_agent`

#### Scenario: No agent available fails clearly

- **WHEN** no `--agent` is given and no `default_agent` is set
- **THEN** the command fails with an error telling the user to add or select an agent

#### Scenario: An agent's memory configuration is stored alongside its definition

- **WHEN** an agent row is read for assembly
- **THEN** the row includes the agent's `memory_config`, used to derive the effective memory configuration

### Requirement: Agents are managed by CLI CRUD

The system SHALL provide `onclaw agent add|list|show|remove|use|edit`. `agent add <name>` SHALL create the `agents` row and, unless `--workspace` is given, SHALL create the agent's default workspace directory at `~/.onclaw/workspace/<name>/`. `agent use <name>` SHALL set the `default_agent` preference. `agent remove` SHALL remove the row but SHALL NOT delete the workspace directory. `agent edit <name>` SHALL update existing agent configurations, accepting optional flags for all agent properties (provider, model, reasoning, reasoning-budget, workspace, description, tools, max-iterations). Only specified fields SHALL be updated; unspecified fields retain their existing values.

#### Scenario: Adding an agent creates its workspace

- **WHEN** the user runs `onclaw agent add coder --provider glm`
- **THEN** an `agents` row `coder` is created and `~/.onclaw/workspace/coder/` exists

#### Scenario: `agent use` sets the default

- **WHEN** the user runs `onclaw agent use coder`
- **THEN** `default_agent` is `coder` and the next `onclaw run` uses it

#### Scenario: Removing an agent keeps the workspace

- **WHEN** the user runs `onclaw agent remove coder`
- **THEN** the `coder` row is deleted and `~/.onclaw/workspace/coder/` is left intact

#### Scenario: Editing an agent updates only specified fields

- **WHEN** the user runs `onclaw agent edit coder --model glm-4.6`
- **THEN** the `coder` row's model and model metadata are updated and all other fields remain unchanged

#### Scenario: Editing an agent with multiple fields

- **WHEN** the user runs `onclaw agent edit coder --model glm-4.6 --reasoning high --max-iterations 30`
- **THEN** the `coder` row's model, model metadata, reasoning_effort, and max_iterations are updated and all other fields remain unchanged