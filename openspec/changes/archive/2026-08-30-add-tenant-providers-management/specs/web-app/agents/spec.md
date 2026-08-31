## MODIFIED Requirements

### Requirement: Structured agent configuration
The agent configuration modal SHALL expose exactly one labeled control per configuration property — name, provider with cascading model list, temperature slider, one-line role, system prompt textarea, tool toggles, skill toggles, MCP server toggles, autonomy selector, and a channel-posting switch — and MUST NOT present raw JSON editing for any structured value. Save SHALL be disabled until the name is longer than one character. Provider options SHALL come from the workspace's configured provider configs (API-driven); unconfigured catalog types MAY appear as disabled entries pointing at Settings → Providers. Changing provider SHALL reset the model to that provider's first entry when the current model is unavailable.

#### Scenario: Deploy a new agent
- **WHEN** the user completes the modal with name "Radar" and confirms
- **THEN** the agent is created in idle status, its (empty) chat opens, and a deployment toast appears

#### Scenario: Provider cascade
- **WHEN** the user switches provider from one configured provider to another while a model of the first is selected
- **THEN** the model selection changes to a model of the newly selected provider

#### Scenario: Unconfigured type entry
- **WHEN** an agent is configured while "Gemini" has no workspace provider config
- **THEN** Gemini appears as a disabled option hinting at Settings → Providers (or is omitted)

#### Scenario: Skill options include workspace skills
- **WHEN** the workspace has custom skills installed beyond the registry
- **THEN** both registry and custom skills appear as toggleable options
