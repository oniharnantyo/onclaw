# Spec Delta

## Purpose

Connect agents to MCP servers: a client manager with stdio/HTTP/SSE transports, probe and tool discovery, namespaced tool naming, and a per-agent server allowlist — every enabled server by default — that merges discovered tools into turns.

## ADDED Requirements

### Requirement: Server transports

MCP server definitions SHALL support stdio (command, arguments, environment), streamable HTTP, and SSE (URL, headers) transports.

#### Scenario: Each transport connects

- **WHEN** a server of each transport type is probed with valid settings
- **THEN** the connection succeeds and the server is reported connected

### Requirement: Probe and tool discovery

The app SHALL connect to a server on demand, discover its tools, and surface connection state — connected with a discovered-tool count, or failed with the reason — in the MCP pane and in agent configuration. A failed probe MUST NOT alter the server's stored definition.

#### Scenario: Healthy server shows state

- **WHEN** a reachable server is probed
- **THEN** its pane row shows connected and the number of discovered tools

#### Scenario: Unreachable server shows the reason

- **WHEN** a server cannot be reached
- **THEN** its pane row shows the failure state with the underlying reason

### Requirement: Namespaced tool naming

Discovered MCP tools SHALL be exposed to the model as `mcp__<server>__<tool>`, sanitized to valid tool names, with deterministic resolution of name collisions.

#### Scenario: Tools are namespaced

- **WHEN** a connected server exposes a tool
- **THEN** the model sees it under the name derived from the server and tool names in the `mcp__<server>__<tool>` form

### Requirement: Server allowlist policy

An agent SHALL see an enabled MCP server's tools when its MCP server list is empty — every enabled server, the default — or names that server; a non-empty list SHALL restrict the agent to the named servers, the same allowlist reading as its tools and skills. The server list — not the built-in tool allowlist — SHALL be the gate for MCP tools, and a disabled server SHALL contribute no tools regardless of the list. Because an empty list means every server rather than none, agent configuration SHALL keep the last server left on pinned (not switchable off); pausing the server in settings is how it leaves every agent.

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

### Requirement: MCP tool execution

MCP tool calls in a turn SHALL execute through the server connection and return the server's result. Connection failures during a turn SHALL surface as tool results; the turn continues and MUST NOT crash.

#### Scenario: Tool call returns a result

- **WHEN** the model calls an MCP tool on a connected server
- **THEN** the server's response is returned as the tool result and recorded on the transcript

#### Scenario: Server dies mid-turn

- **WHEN** the server connection fails while a tool call is in flight
- **THEN** the tool result reports the failure and the turn continues
