## MODIFIED Requirements

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
