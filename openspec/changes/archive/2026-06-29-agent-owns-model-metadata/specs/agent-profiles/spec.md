## MODIFIED Requirements

### Requirement: An agent owns its model and metadata and resolves to an effective provider profile without holding credentials

The system SHALL resolve a selected agent to an effective provider profile by copying its
referenced provider profile and overlaying the agent's `model`, `reasoning_effort`, and
model metadata (context window, thinking flag, input modalities). The provider profile
SHALL NOT contribute a model. The effective model SHALL resolve as the per-run `--model`
flag, then the agent row's model, then the configured default model (`config.model`); if
none is present, building SHALL fail with a clear error. The runtime context window SHALL
be sourced from the agent's model metadata, then the configured `max_context_tokens`, then
a built-in default. The effective profile SHALL then be built via the existing provider
adapter. An agent row SHALL NOT store an API key; the key SHALL remain in the referenced
provider profile.

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

### Requirement: Per-run flags override the selected agent

The system SHALL accept `--provider`, `--model`, and `--reasoning` flags on `onclaw
run`/`chat` that override the selected agent's values for that single invocation only.
Precedence SHALL be explicit flags > agent row > configured default model. The provider
profile SHALL NOT contribute a model or context window.

#### Scenario: A per-run flag overrides the agent's model

- **WHEN** the user runs `onclaw run --agent coder --model glm-4-air "hi"`
- **THEN** this invocation uses `glm-4-air` and the `coder` row's model is unchanged

## ADDED Requirements

### Requirement: An agent stores discovered metadata for its chosen model

The system SHALL store, on each agent row, metadata for the agent's chosen model: context
window (tokens), a thinking/reasoning flag, and input modalities. The metadata SHALL be
discoverable at `agent add`/`edit` time by enumerating the referenced provider's available
models and enriching the selected model via the model-discovery capability. `agent add`
SHALL run an interactive model picker when no `--model` is supplied; when `--model` is
supplied, its metadata SHALL be resolved non-interactively. `agent list` and `agent show`
SHALL render the model and its metadata.

#### Scenario: Adding an agent without --model opens the picker

- **WHEN** the user runs `onclaw agent add coder --provider glm` with no `--model`
- **THEN** the command enumerates `glm`'s available models, lets the user select one, and stores the selected model plus its discovered metadata on the `coder` row

#### Scenario: Adding an agent with --model resolves metadata silently

- **WHEN** the user runs `onclaw agent add coder --provider glm --model glm-4-air`
- **THEN** the `coder` row stores `glm-4-air` and its discovered context window, thinking flag, and input modalities without an interactive prompt

#### Scenario: agent show renders metadata

- **WHEN** the user runs `onclaw agent show coder`
- **THEN** the output includes the model and its context window, thinking flag, and input modalities