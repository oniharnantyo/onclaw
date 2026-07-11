## ADDED Requirements

### Requirement: The web app uses URL-based routing

The system SHALL serve the management UI through a client-side router with one URL per primary surface, so that views are deep-linkable and browser back/forward navigation works. Agent configuration SHALL have its own URL rather than being a transient dialog. The default entry path SHALL redirect to the chat surface.

#### Scenario: An agent configuration page is deep-linkable

- **WHEN** a user navigates directly to the URL for a specific agent
- **THEN** that agent's configuration page loads without first visiting another view

#### Scenario: Browser navigation works across surfaces

- **WHEN** a user moves between the chat, agents list, and an agent configuration page
- **THEN** the browser back and forward buttons move between those views as expected

### Requirement: Agent configuration is a dedicated page, not a dialog

Creating and editing an agent SHALL happen on a dedicated page with sections for the agent's identity and model, tools, hooks, skills, MCP servers, memory, and persona — rather than a modal dialog. Global-scope resources (hooks, skills, and the MCP/tool registries) SHALL be manageable from the same page rendered for a reserved `global` scope, and the top-level navigation entries for those resources SHALL route there.

#### Scenario: Creating an agent opens a page

- **WHEN** the user chooses to add an agent
- **THEN** a dedicated create page opens at a distinct URL, not a modal

#### Scenario: Global resources are managed on the same page

- **WHEN** the user opens the global hooks view from navigation
- **THEN** the agent configuration page renders in its global scope showing global hooks

### Requirement: Editable configuration is rendered as structured fields

Every editable configuration exposed in the agent page — including the per-agent memory configuration and MCP server selection — SHALL be rendered as one typed form field per property (selects, checkboxes, number and text inputs, each labeled with a tooltip and inline validation), driven by the configuration's schema. The UI SHALL NOT expose editable structured configuration as a single raw-JSON textarea. Free-text values whose content is genuinely unstructured (system prompt, persona markdown) MAY remain textareas.

#### Scenario: The memory configuration form uses one field per property

- **WHEN** the user edits an agent's memory configuration
- **THEN** each feature toggle is a switch and each parameter is a typed input, and saving persists the structured configuration

#### Scenario: A raw-JSON configuration editor is not offered

- **WHEN** the user edits per-agent memory or MCP configuration
- **THEN** no raw-JSON textarea is presented as the editing surface for that structured configuration