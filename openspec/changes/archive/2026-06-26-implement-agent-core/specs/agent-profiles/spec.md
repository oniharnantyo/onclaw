# agent-profiles

## Purpose

Let a user create more than one named agent, select which one runs, and have each agent select its own model and reasoning effort — without duplicating credentials or running more than one agent at a time.

## ADDED Requirements

### Requirement: Named agents are stored in an `agents` table and selected per run

The system SHALL store agent definitions in an `agents` table (name, provider, model, reasoning_effort, system_prompt, tools, max_iterations, workspace, enabled). `onclaw run` and `onclaw chat` SHALL select the agent to use from a `--agent <name>` flag, or when absent from a `default_agent` preference. The selected agent SHALL be the only agent that runs for that invocation. Running with no `--agent` and no `default_agent` SHALL fail with a clear error.

#### Scenario: The default agent is used when `--agent` is absent

- **WHEN** a `default_agent` preference is set to `coder` and the user runs `onclaw run "hi"`
- **THEN** the `coder` agent runs

#### Scenario: `--agent` selects a named agent

- **WHEN** the user runs `onclaw run --agent reviewer "hi"`
- **THEN** the `reviewer` agent runs, regardless of `default_agent`

#### Scenario: No agent available fails clearly

- **WHEN** no `--agent` is given and no `default_agent` is set
- **THEN** the command fails with an error telling the user to add or select an agent

### Requirement: An agent resolves to an effective provider profile without holding credentials

The system SHALL resolve a selected agent to an effective provider profile by copying its referenced provider profile and overlaying the agent's `model` and `reasoning_effort` (an empty value falls back to the profile's setting). The effective profile SHALL then be built via the existing provider adapter. An agent row SHALL NOT store an API key; the key SHALL remain in the referenced provider profile.

#### Scenario: The agent's model and effort override the profile

- **WHEN** an agent references provider `glm` (model `glm-4.6`, default effort) but sets `model: glm-4-air` and `reasoning_effort: high`
- **THEN** the built ChatModel targets `glm-4-air` with high reasoning effort

#### Scenario: Empty overrides fall back to the profile

- **WHEN** an agent references provider `glm` and leaves `model` and `reasoning_effort` empty
- **THEN** the built ChatModel uses the profile's model and the provider's default effort

#### Scenario: A disabled or missing provider fails clearly

- **WHEN** an agent references a provider that does not exist or is disabled
- **THEN** building the agent fails with a clear error and no ChatModel is produced

### Requirement: Agents are managed by CLI CRUD

The system SHALL provide `onclaw agent add|list|show|remove|use`. `agent add <name>` SHALL create the `agents` row and, unless `--workspace` is given, SHALL create the agent's default workspace directory at `~/.onclaw/workspace/<name>/`. `agent use <name>` SHALL set the `default_agent` preference. `agent remove` SHALL remove the row but SHALL NOT delete the workspace directory.

#### Scenario: Adding an agent creates its workspace

- **WHEN** the user runs `onclaw agent add coder --provider glm`
- **THEN** an `agents` row `coder` is created and `~/.onclaw/workspace/coder/` exists

#### Scenario: `agent use` sets the default

- **WHEN** the user runs `onclaw agent use coder`
- **THEN** `default_agent` is `coder` and the next `onclaw run` uses it

#### Scenario: Removing an agent keeps the workspace

- **WHEN** the user runs `onclaw agent remove coder`
- **THEN** the `coder` row is deleted and `~/.onclaw/workspace/coder/` is left intact

### Requirement: Reasoning effort is a normalized value mapped per provider

The system SHALL store reasoning effort as one of `low`, `medium`, `high`, or empty (provider default), on the agent row and via a `--reasoning` flag. The provider adapter SHALL map the normalized value to the provider's native request field. A value the provider does not support SHALL be ignored (no effort sent) rather than erroring.

#### Scenario: High effort reaches the OpenAI-compatible request

- **WHEN** an agent with `reasoning_effort: high` is built via the OpenAI-compatible adapter
- **THEN** the outbound request carries the provider's native high-effort field

### Requirement: Per-run flags override the selected agent

The system SHALL accept `--provider`, `--model`, and `--reasoning` flags on `onclaw run`/`chat` that override the selected agent's values for that single invocation only. Precedence SHALL be explicit flags > agent row > provider-profile defaults.

#### Scenario: A per-run flag overrides the agent's model

- **WHEN** the user runs `onclaw run --agent coder --model glm-4-air "hi"`
- **THEN** this invocation uses `glm-4-air` and the `coder` row's model is unchanged

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