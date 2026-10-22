# Spec Delta

## MODIFIED Requirements

### Requirement: Edit agent configuration

Agent configuration SHALL include, at minimum: provider and model selection; an optional default reasoning effort whose options are limited to those the selected model supports, reset when a model without effort support is chosen; the agent's three prompt documents (IDENTITY, SOUL, BOOTSTRAP) in editable multi-line editors presented behind a file rail, a regenerate control for the documents, enabled skills, enabled tools, MCP server opt-ins, and hooks. Changes MUST be saved atomically and reflected in subsequent turns without requiring an app restart. The configured effort is the default effort for the agent's sessions whenever a session carries no effort override.

#### Scenario: Model change applies to next turn

- **WHEN** the user switches an agent's model and sends a message in its chat
- **THEN** the turn executes on the newly selected model

#### Scenario: Configuration sections

- **WHEN** the user opens an agent's configuration
- **THEN** the provider/model with its effort default, prompt documents with regenerate, skills, tools, MCP, and hooks sections are all present and editable

#### Scenario: Document editor reflects the workspace file

- **WHEN** the user opens an agent's prompt document editors
- **THEN** each editor shows the current content of the corresponding file in the agent's workspace directory and saving rewrites that file

#### Scenario: Effort options follow the model

- **WHEN** the user selects a reasoning-capable model in the Model section
- **THEN** the effort control offers only that model's supported options, and switching to a model without effort support clears the saved effort

#### Scenario: Agent effort is the session default

- **WHEN** a new chat session starts for an agent with a configured default effort and no session override
- **THEN** the first turn applies the agent's default effort