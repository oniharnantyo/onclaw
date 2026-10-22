# agent-mcp Specification

## Purpose

Connect agents to MCP servers: a client manager with stdio/HTTP/SSE transports, probe and tool discovery, namespaced tool naming, and a per-agent server allowlist — every enabled server by default — that merges discovered tools into turns.

## Requirements

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

### Requirement: OAuth server authentication

The app SHALL support OAuth 2.1 client authentication for HTTP and SSE MCP servers whose protection advertises OAuth via a `WWW-Authenticate` header naming a protected-resource metadata document. The app SHALL discover the authorization server from that document, register itself dynamically when it has no stored registration for the server, and authorize through the user's system browser with an authorization-code flow using PKCE and a loopback redirect. Obtained tokens SHALL persist across restarts and SHALL refresh automatically without user interaction. Each server SHALL support an explicit Authentication setting of none (static headers) or OAuth; the Authorization flow SHALL NOT block or extend the bounded connect and probe paths.

#### Scenario: First authorization connects the server

- **WHEN** the user authorizes an HTTP server that advertises OAuth and completes the browser flow
- **THEN** the tokens are stored and the server connects and reports connected with its discovered tools

#### Scenario: Expired tokens refresh headlessly

- **WHEN** a server with a stored, expired access token is used or refreshed after restart, including in server mode with no browser available
- **THEN** the client refreshes the token and connects without user interaction

#### Scenario: Failed refresh returns to authorization-required

- **WHEN** a stored refresh token is no longer accepted
- **THEN** the server reports the authorization-required state and does not report a generic failure

#### Scenario: Disconnect revokes and clears

- **WHEN** the user disconnects an OAuth-authenticated server
- **THEN** the stored tokens are deleted, the token is revoked at the advertised revocation endpoint when one exists, the registration is kept, and the server reports the authorization-required state

### Requirement: Authorization-required connection state

The probe SHALL distinguish servers that require user authorization from generic connection failures: a connection rejected as unauthenticated whose protection advertises OAuth metadata SHALL persist an authorization-required state, and any other failed connection SHALL persist the failure state with its reason. An authorization-required server SHALL NOT be treated as healthy by agents.

#### Scenario: Unauthenticated OAuth server probes as authorization-required

- **WHEN** a probe of a server advertising OAuth is rejected with an unauthenticated response and valid protected-resource metadata
- **THEN** the server persists the authorization-required state rather than the failure state

#### Scenario: Authorizing clears the state

- **WHEN** the authorization flow completes for a server in the authorization-required state
- **THEN** the server is re-probed and reports connected or, if it remains unreachable, the failure state with its reason

### Requirement: Connection refresh

Refreshing a server SHALL first discard its cached runtime connection and then re-dial the stored definition, so agents use fresh credentials immediately after a refresh rather than after a cache-retry or idle window. Refreshing all SHALL apply this to every enabled server. The stored definition SHALL NOT be altered by a refresh.

#### Scenario: Refresh swaps live credentials

- **WHEN** a user refreshes a server after its tokens changed
- **THEN** subsequent agent turns use a connection established with the current credentials, and the row shows the fresh outcome

#### Scenario: Refresh failure is shown, definition untouched

- **WHEN** a refresh cannot establish a connection
- **THEN** the row shows the failure or authorization-required outcome and the server definition is unchanged
