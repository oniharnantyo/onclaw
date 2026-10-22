# Spec Delta

## Purpose

Provide the agents menu: a card grid for browsing agents and full agent configuration (provider/model, system prompt, skills, tools, MCP servers, hooks) ported from onclaw-v2's agent editor, using generated avatars from react-nice-avatar.

## ADDED Requirements

### Requirement: Agent cards

The agents menu SHALL display each agent as a card showing its generated avatar (react-nice-avatar), name, configured model, and enabled state. Selecting a card MUST navigate to that agent's chat; an edit affordance MUST open the agent's configuration.

#### Scenario: Card reflects configuration

- **WHEN** an agent's model is changed in its configuration
- **THEN** its card shows the updated model

#### Scenario: Card opens chat

- **WHEN** the user selects an agent card's chat affordance
- **THEN** the chat menu opens that agent's most recent session

### Requirement: Create agent

The user SHALL be able to create a new agent by providing at least a name; the agent MUST be persisted and immediately usable in chat once a provider and model are selected. A new agent SHALL receive a deterministic generated avatar derived from its identity.

#### Scenario: Create minimal agent

- **WHEN** the user creates an agent named "Scout"
- **THEN** the agent appears in the cards grid with its own generated avatar and can be configured

### Requirement: Edit agent configuration

Agent configuration SHALL include, at minimum: provider and model selection, system prompt, enabled skills, enabled tools, MCP server opt-ins, and hooks. Changes MUST be saved atomically and reflected in subsequent turns without requiring an app restart.

#### Scenario: Model change applies to next turn

- **WHEN** the user switches an agent's model and sends a message in its chat
- **THEN** the turn executes on the newly selected model

#### Scenario: Configuration sections

- **WHEN** the user opens an agent's configuration
- **THEN** the provider/model, system prompt, skills, tools, MCP, and hooks sections are all present and editable

### Requirement: Delete agent

The user SHALL be able to delete an agent. Deleting an agent MUST require confirmation and MUST NOT delete the agent's past session history.

#### Scenario: Delete with confirmation

- **WHEN** the user confirms deletion of an agent
- **THEN** the agent is removed from the cards grid while its sessions remain in the data store

### Requirement: Theme parity

The agents menu SHALL render correctly in both the light and dark daisyUI themes, including cards, avatars, and all configuration sections.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while viewing the agents menu
- **THEN** all cards and configuration sections render legibly in the selected theme
