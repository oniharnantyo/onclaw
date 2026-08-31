# web-app/agents Specification

## Purpose

The agents screen — the roster of workspace agents as cards — and the structured configuration modal used to deploy a new agent or edit an existing one.

## Requirements

### Requirement: Agents roster
The agents screen SHALL list every agent in the active workspace as a card showing name, live status dot, model and temperature in monospace, one-line role, granted tool chips (up to four), last-active timestamp, and actions to open the chat or configure the agent. Agents in error state SHALL show a "Needs attention" notice. The grid SHALL flow 1 column below 640px, 2 at ≥640px, and 3 at ≥1280px.

#### Scenario: Error-state agent
- **WHEN** agent "Warden" has status `error`
- **THEN** its card shows a danger-colored "Needs attention — see latest run" notice

#### Scenario: Empty roster
- **WHEN** the workspace has no agents
- **THEN** the grid shows an empty state inviting the user to deploy one

### Requirement: Structured agent configuration
The agent configuration modal SHALL expose exactly one labeled control per configuration property — name, provider with cascading model list, temperature slider, one-line role, system prompt textarea, tool toggles, skill toggles, MCP server toggles, autonomy selector, and a channel-posting switch — and MUST NOT present raw JSON editing for any structured value. Save SHALL be disabled until the name is longer than one character. Provider options SHALL come from the workspace's configured provider configs (API-driven); unconfigured catalog types MAY appear as disabled entries pointing at Settings → Providers. Changing provider SHALL reset the model to that provider's first entry when the current model is unavailable.

#### Scenario: Deploy a new agent
- **WHEN** the user completes the modal with name "Radar" and confirms
- **THEN** the agent is created in idle status, its (empty) chat opens, and a deployment toast appears

#### Scenario: Provider cascade
- **WHEN** the user switches provider from one configured provider to another while a model of the first is selected
- **THEN** the model selection changes to a model of the newly selected provider

#### Scenario: Skill options include workspace skills
- **WHEN** the workspace has custom skills installed beyond the registry
- **THEN** both registry and custom skills appear as toggleable options

#### Scenario: Unconfigured type entry
- **WHEN** an agent is configured while "Gemini" has no workspace provider config
- **THEN** Gemini appears as a disabled option hinting at Settings → Providers (or is omitted)

### Requirement: Agent status semantics
Agent status SHALL display as Running (pulsing dot), Idle (muted dot), or error state ("Needs attention"), consistently in the sidebar, chat header, and agent cards.

#### Scenario: Running agent indicator
- **WHEN** an agent is running
- **THEN** its status dot pulses and the chat header reads "Running · last active …"
