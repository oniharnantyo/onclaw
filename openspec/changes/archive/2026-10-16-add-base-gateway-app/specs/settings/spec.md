# Spec Delta

## Purpose

Provide the settings menu with panes for providers, skills, tools, hooks, and MCP servers — the shared configuration that agents draw on — ported from onclaw-v2's settings with the tenancy-related panes removed.

## ADDED Requirements

### Requirement: Providers pane

The settings menu SHALL let the user add, edit, and remove providers. Each provider entry SHALL hold its API credential and SHALL expose the models available from it for selection by agents. Credentials MUST be persisted with the rest of configuration.

#### Scenario: Add a provider

- **WHEN** the user adds a provider with a valid API credential
- **THEN** the provider appears in the list and its models become selectable in agent configuration

#### Scenario: Remove a provider

- **WHEN** the user removes a provider that no agent currently uses
- **THEN** the provider disappears from settings and from agent model options

### Requirement: Skills pane

The settings menu SHALL list skills discovered from the skills directory, showing each skill's name and description, and SHALL allow enabling or disabling a skill globally. Disabled skills MUST NOT be offered to agents.

#### Scenario: Disable a skill

- **WHEN** the user disables a skill
- **THEN** that skill no longer appears as selectable in agent configuration or the composer's skill menu

### Requirement: Tools pane

The settings menu SHALL list the available tools with their descriptions and SHALL allow enabling or disabling each tool. Disabled tools MUST NOT be offered to agents.

#### Scenario: Toggle a tool

- **WHEN** the user disables a tool that an agent had enabled
- **THEN** the agent's subsequent turns no longer offer that tool

### Requirement: Hooks pane

The settings menu SHALL let the user view, add, edit, and remove hooks, including the events they bind to and a test affordance that exercises the hook and reports the outcome.

#### Scenario: Test a hook

- **WHEN** the user runs the test action on a hook
- **THEN** the hook executes and its outcome (success or failure) is displayed

### Requirement: MCP pane

The settings menu SHALL let the user add, edit, and remove MCP server definitions (transport, command or URL, arguments, environment) and SHALL show the connection state of each server. Agents opt in to servers from their own configuration.

#### Scenario: Add an MCP server

- **WHEN** the user adds a valid MCP server definition
- **THEN** the server is listed with its connection state and becomes available for agents to opt into

### Requirement: Theme parity

The settings menu SHALL render correctly in both the light and dark daisyUI themes across all panes, forms, and lists.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while in settings
- **THEN** all panes render legibly in the selected theme
