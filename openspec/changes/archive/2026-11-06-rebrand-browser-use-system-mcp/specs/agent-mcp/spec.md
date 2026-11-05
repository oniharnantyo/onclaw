# agent-mcp Specification (delta)

## ADDED Requirements

### Requirement: System-provided server

The MCP client manager SHALL provide exactly one built-in server definition — Browser Use, a stdio server spawned from the app's own binary — synthesized by the app rather than stored as a user server row. The system server SHALL be non-deletable, non-editable, and always enabled, and SHALL otherwise behave exactly like a user server: probed on demand, reported with connection state and discovered-tool count, namespaced as `mcp__browser-use__<tool>`, and gated by the standard per-agent server allowlist (available to every agent by default). The MCP pane SHALL render its row with a distinct System badge and no edit or remove affordances, with an action opening the Browser Use settings page.

#### Scenario: System row appears without user setup

- **WHEN** the MCP pane is rendered on a fresh install
- **THEN** a Browser Use row with a System badge is present, probed to connected, without any user-created definition

#### Scenario: System row cannot be removed or edited

- **WHEN** the user inspects the Browser Use row's actions
- **THEN** no remove or edit affordance exists, and an "open settings" action navigates to the Browser Use pane

#### Scenario: Agent allowlist gates the system server

- **WHEN** an agent's server allowlist excludes Browser Use
- **THEN** that agent's turns are offered no `mcp__browser-use__*` tools while other agents keep them by default
