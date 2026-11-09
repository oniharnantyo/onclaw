## ADDED Requirements

### Requirement: System server registry

The MCP client manager SHALL hold a registry of built-in system server definitions — each a stdio server spawned from the app's own binary with a fixed identifier, display name, and supervision settings — synthesized by the app rather than stored as user server rows. Every system server SHALL be non-deletable, non-editable, and always enabled, and SHALL otherwise behave exactly like a user server: probed on demand, reported with connection state and discovered-tool count, and namespaced as `mcp__<server id>__<tool>`. Adding a system server MUST NOT require changes to user server storage.

#### Scenario: Multiple system rows render

- **WHEN** the MCP pane is rendered with two registered system servers
- **THEN** each appears as its own row with the System badge, its live connection state, and no edit or remove affordances

## MODIFIED Requirements

### Requirement: System-provided server

The MCP client manager SHALL provide built-in system server definitions synthesized by the app rather than stored as user server rows — Browser Use (`browser-use`, browser automation) and Computer Use (`computer-use`, desktop control) — each a stdio server spawned from the app's own binary. Each system server SHALL be non-deletable, non-editable, and always enabled, and SHALL otherwise behave exactly like a user server: probed on demand, reported with connection state and discovered-tool count, namespaced as `mcp__<server id>__<tool>`, and gated by the standard per-agent server allowlist under the opt-in rule below. The MCP pane SHALL render each system row with a distinct System badge and no edit or remove affordances, with an action opening that server's settings page. Browser Use is offered by default; Computer Use is opt-in (see the allowlist policy).

#### Scenario: System row appears without user setup

- **WHEN** the MCP pane is rendered on a fresh install
- **THEN** a Browser Use row with a System badge is present, probed to connected, without any user-created definition

#### Scenario: System row cannot be removed or edited

- **WHEN** the user inspects a system row's actions
- **THEN** no remove or edit affordance exists, and an "open settings" action navigates to that server's settings pane

#### Scenario: Agent allowlist gates the system server

- **WHEN** an agent's server allowlist excludes Browser Use
- **THEN** that agent's turns are offered no `mcp__browser-use__*` tools while other agents keep them by default

### Requirement: Server allowlist policy

An agent SHALL see an enabled MCP server's tools when its MCP server list is empty — every enabled server, the default — or names that server; a non-empty list SHALL restrict the agent to the named servers, the same allowlist reading as its tools and skills. The server list — not the built-in tool allowlist — SHALL be the gate for MCP tools, and a disabled server SHALL contribute no tools regardless of the list. Because an empty list means every server rather than none, agent configuration SHALL keep the last server left on pinned (not switchable off); pausing the server in settings is how it leaves every agent. The system server registry MAY mark a system server opt-in: an opt-in system server SHALL be offered only to agents that name it in their list, and SHALL be withheld even when an agent's list is empty — adding an opt-in system server MUST NOT silently widen existing agents.

#### Scenario: Servers are offered by default

- **WHEN** an agent's MCP server list is empty and an enabled, connected server exists
- **THEN** its turns are offered the server's discovered tools

#### Scenario: A restricted agent sees only the named servers

- **WHEN** an agent's MCP server list names some enabled servers but not another
- **THEN** its turns are offered only the named servers' tools and the unnamed server is never contacted

#### Scenario: Disabled servers are withheld

- **WHEN** a server is disabled in settings
- **THEN** none of its tools appear on any agent's turns, named or not

#### Scenario: The last server on is pinned

- **WHEN** exactly one server is left on in an agent's configuration
- **THEN** its toggle is disabled and marked always on, so turning servers off never empties the stored list

#### Scenario: Opt-in system server is withheld from empty lists

- **WHEN** Computer Use (an opt-in system server) is registered and an agent's server list is empty
- **THEN** that agent is offered no `mcp__computer-use__*` tools until it names `computer-use` in its list
