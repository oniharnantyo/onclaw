## MODIFIED Requirements

### Requirement: Agent configuration is a dedicated page, not a dialog

Creating and editing an agent SHALL happen on a dedicated page rather than a modal dialog. The
create form SHALL be a focused identity+model form — agent name, provider, model and its discovered
metadata, reasoning control, system prompt, workspace, max iterations, and default flag — and SHALL
NOT present resource sections (tools, hooks, skills, MCP servers, memory, persona) at create time.
Those resource sections SHALL appear only when editing an existing agent. A newly created agent
SHALL receive all globally-enabled builtin tools by default, expressed as an empty per-agent tool
allowlist, matching the `onclaw agent add` subcommand (which sets no allowlist). Global-scope
resources (hooks, skills, and the MCP/tool registries) SHALL be manageable from the same page
rendered for a reserved `global` scope, and the top-level navigation entries for those resources
SHALL route there.

#### Scenario: Creating an agent opens a focused page

- **WHEN** the user chooses to add an agent
- **THEN** a dedicated create page opens at a distinct URL (not a modal) showing only the
  identity+model form, with no tools, hooks, skills, MCP, memory, or persona sections

#### Scenario: A new agent defaults to all builtin tools

- **WHEN** the user creates an agent through the web create form and saves
- **THEN** the agent's per-agent tool allowlist is empty and the agent is offered every
  globally-enabled builtin tool at run time, identical to `onclaw agent add <name> --provider <p>`

#### Scenario: Resource sections appear when editing an existing agent

- **WHEN** the user opens an existing agent's configuration page
- **THEN** the page presents the tools, hooks, skills, MCP, memory, and persona sections scoped to
  that agent, and the tools section renders an empty allowlist as "all tools enabled"

#### Scenario: Global resources are managed on the same page

- **WHEN** the user opens the global hooks view from navigation
- **THEN** the agent configuration page renders in its global scope showing global hooks
