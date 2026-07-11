# agent-profiles

## Purpose

Let a user create more than one named agent, select which one runs, and have each agent select its own model and reasoning effort — without duplicating credentials or running more than one agent at a time.
## Requirements
### Requirement: Named agents are stored in an `agents` table and selected per run

The system SHALL store agent definitions in an `agents` table (name, provider, model, model metadata, reasoning control, system prompt, workspace, tools, max iterations, memory configuration, enabled). `onclaw run` and `onclaw chat` SHALL select the agent to use from a `--agent <name>` flag, or when absent from a `default_agent` preference. The selected agent SHALL be the only agent that runs for that invocation. Running with no `--agent` and no `default_agent` SHALL fail with a clear error.

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

### Requirement: An agent owns its model and metadata and resolves to an effective provider profile without holding credentials

The system SHALL resolve a selected agent to an effective provider profile by copying its referenced provider profile and overlaying the agent's `model`, `reasoning_effort`, and model metadata (context window, thinking flag, input modalities). The provider profile SHALL NOT contribute a model. The effective model SHALL resolve as the per-run `--model` flag, then the agent row's model, then the configured default model (`config.model`); if none is present, building SHALL fail with a clear error. The runtime context window SHALL be sourced from the agent's model metadata, then the configured `max_context_tokens`, then a built-in default. The effective profile SHALL then be built via the existing provider adapter. An agent row SHALL NOT store an API key; the key SHALL remain in the referenced provider profile.

#### Scenario: The agent's model and metadata drive the build

- **WHEN** an agent references provider `glm` and sets model `glm-4-air` with context_window 128000 and `reasoning_effort: high`
- **THEN** the built ChatModel targets `glm-4-air` with high reasoning effort and the runtime uses a 128000-token context window

#### Scenario: Empty agent model falls back to the configured default, not the provider

- **WHEN** an agent references provider `glm` and leaves its model empty, and `config.model` is `glm-4.6`
- **THEN** the built ChatModel uses `glm-4.6`; no provider model is consulted

#### Scenario: No model anywhere fails clearly

- **WHEN** an agent has no model, no per-run `--model` flag is given, and no configured default model exists
- **THEN** building the agent fails with a clear error and no ChatModel is produced

#### Scenario: A disabled or missing provider fails clearly

- **WHEN** an agent references a provider that does not exist or is disabled
- **THEN** building the agent fails with a clear error and no ChatModel is produced

### Requirement: Agents are managed by CLI CRUD

The system SHALL provide `onclaw agent add|list|show|remove|use|edit`. `agent add <name>` SHALL create the `agents` row and, unless `--workspace` is given, SHALL create the agent's default workspace directory at `~/.onclaw/workspace/<name>/`. `agent use <name>` SHALL set the `default_agent` preference. `agent remove` SHALL remove the row but SHALL NOT delete the workspace directory. `agent edit <name>` SHALL update existing agent configurations, accepting optional flags for all agent properties (provider, model, reasoning, reasoning-budget, workspace, system-prompt, tools, max-iterations). Only specified fields SHALL be updated; unspecified fields retain their existing values.

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

### Requirement: Reasoning effort is a normalized value mapped per provider

The system SHALL store a reasoning control on the agent, chosen from the model's discovered `reasoning_options`: an `effort` enum value (e.g. `low`, `medium`, `high`, `minimal`, `xhigh`, `max`), a `budget_tokens` integer within the model's declared range, or a `toggle` of `on`/`off`. The chosen value SHALL be set via `--reasoning` (effort enum or toggle) or `--reasoning-budget` (budget tokens) on `agent add`/`edit`. The system SHALL strictly validate the value against the selected model's declared options and SHALL reject — rather than silently ignore — any value the model does not support, or any reasoning control on a non-reasoning model. The provider adapter SHALL map the validated control to the provider's native request field.

#### Scenario: A supported effort value reaches the request

- **WHEN** an agent's model supports effort `high` and the agent stores `reasoning_effort: high`
- **THEN** the built ChatModel sends the provider's native high-effort field

#### Scenario: An unsupported value is rejected at edit time

- **WHEN** the user sets `--reasoning xhigh` on a model whose supported effort values are `["low","medium","high"]`
- **THEN** the command fails listing the valid values, and no change is saved

### Requirement: Per-run flags override the selected agent

The system SHALL accept `--provider`, `--model`, and `--reasoning` flags on `onclaw run`/`chat` that override the selected agent's values for that single invocation only. Precedence SHALL be explicit flags > agent row > configured default model. The provider profile SHALL NOT contribute a model or context window.

#### Scenario: A per-run flag overrides the agent's model

- **WHEN** the user runs `onclaw run --agent coder --model glm-4-air "hi"`
- **THEN** this invocation uses `glm-4-air` and the `coder` row's model is unchanged

### Requirement: An agent stores discovered metadata for its chosen model

The system SHALL store, on each agent row, metadata for the agent's chosen model: context window (tokens), a thinking/reasoning flag, input modalities, and the model's `reasoning_options` (the control types the model supports: `effort` with its values, `budget_tokens` with its min/max, and/or `toggle`). The metadata SHALL be discoverable at `agent add`/`edit` time by enumerating the referenced provider's available models and enriching the selected model via the model-discovery capability. The agent SHALL store the user's chosen reasoning control: an effort enum value or toggle in `reasoning_effort`, and a budget-tokens count in `reasoning_budget_tokens`. `agent add` SHALL run an interactive model picker when no `--model` is supplied, and SHALL prompt for the reasoning effort when the selected model is a reasoning model; when `--model` is supplied, its metadata SHALL be resolved non-interactively. `agent list` and `agent show` SHALL render the model, its metadata, and the stored reasoning control.

#### Scenario: Adding an agent without --model opens the picker and prompts for effort

- **WHEN** the user runs `onclaw agent add coder --provider openai` with no `--model` and selects a reasoning model
- **THEN** the command enumerates the provider's models, lets the user select one, prompts for the reasoning effort, and stores the selected model, its metadata, and the chosen effort on the `coder` row

#### Scenario: Adding an agent with --model resolves metadata silently

- **WHEN** the user runs `onclaw agent add coder --provider openai --model gpt-5`
- **THEN** the `coder` row stores `gpt-5` and its discovered context window, thinking flag, input modalities, and reasoning options without an interactive prompt

#### Scenario: agent show renders metadata and reasoning control

- **WHEN** the user runs `onclaw agent show coder`
- **THEN** the output includes the model, its context window, thinking flag, input modalities, reasoning options, and the stored reasoning control

### Requirement: A builtin default agent always exists

The system SHALL provide a builtin default agent (the "master") that is always present and is
the initial value of `default_agent`. Because it always exists, `onclaw run`/`chat` SHALL
succeed out of the box after provider setup even when no user-defined agents exist. The
master is the protagonist agent; user-defined named agents are optional specializations. The
master's persona and learned memory are shared globally across agents (cross-ref
`agent-identity`).

#### Scenario: A fresh install runs without creating an agent

- **WHEN** onboarding completes provider setup and the user runs `onclaw run "hi"` without adding any agent
- **THEN** the builtin master agent runs rather than failing with "no agent available"

### Requirement: An agent stores its own memory configuration

The system SHALL persist a per-agent memory configuration on the agent row as a JSON document (`agents.memory_config`). The configuration SHALL round-trip through agent create and edit without loss, and SHALL default to an empty document for agents created before the column existed. The configuration SHALL be editable through the agent management UI and the agent API.

#### Scenario: A saved memory configuration is preserved across edits

- **WHEN** an agent's memory configuration is updated and the agent is later edited for an unrelated field
- **THEN** the previously saved memory configuration is unchanged

#### Scenario: A pre-existing agent gets an empty configuration

- **WHEN** the `memory_config` column is added to a database that already contains agents
- **THEN** every existing agent's `memory_config` is `{}` and the agent behaves as the global defaults

### Requirement: An agent stores a per-agent maximum context window

The system SHALL store a per-agent `max_context_tokens` on the agent profile. A value of `0` SHALL mean "inherit." When assembling an agent, the system SHALL resolve the effective context window as the first available of: the agent's `max_context_tokens` when greater than zero, the global `max_context_tokens` config value when greater than zero, then the selected model's discovered context window. The per-agent value SHALL override the global config value, which SHALL override the model default. Existing agents SHALL default to `0` so current global-driven behavior is preserved.

#### Scenario: A per-agent override wins over the global value

- **WHEN** an agent has `max_context_tokens = 32000` and the global config is `64000`
- **THEN** the agent assembles with an effective context window of `32000`

#### Scenario: An unset agent inherits the global value

- **WHEN** an agent has `max_context_tokens = 0` and the global config is `64000`
- **THEN** the agent assembles with an effective context window of `64000` (unchanged from today)

#### Scenario: Both unset falls back to the model default

- **WHEN** an agent has `max_context_tokens = 0`, the global config is `0`/unset, and the model's discovered context window is `128000`
- **THEN** the agent assembles with an effective context window of `128000`

#### Scenario: Existing agents are unaffected by the migration

- **WHEN** the `agents.max_context_tokens` column is added
- **THEN** every existing row has `max_context_tokens = 0` and preserves its prior behavior

