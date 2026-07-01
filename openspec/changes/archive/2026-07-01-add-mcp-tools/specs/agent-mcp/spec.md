# agent-mcp

## Purpose

Let agents discover and invoke tools exposed by external MCP (Model Context Protocol) servers
configured and managed by the user, over the stdio and Streamable HTTP transports (with a
legacy SSE fallback). MCP servers are persisted, managed via the CLI, hot-reloaded at
runtime, and their tools flow through the same redaction and allowlist as built-in tools.

## Requirements

### Requirement: MCP servers are persisted and managed via the CLI

The system SHALL persist MCP server definitions in a SQLite store keyed by name, each with a
transport (`stdio`, `http`, or `sse`), the stdio command/args (or remote URL), an environment
map, and an enabled flag. The system SHALL provide `onclaw mcp add`, `list`, `remove`, and
`test` commands. A server added or removed SHALL be reflected on the next agent run without a
restart (hot-reload).

#### Scenario: A stdio server is added

- **WHEN** the user runs `onclaw mcp add fs -- npx -y @modelcontextprotocol/server-filesystem /tmp`
- **THEN** the server is persisted with transport `stdio`, command `npx`, and the given args

#### Scenario: A streamable-HTTP server is added

- **WHEN** the user runs `onclaw mcp add remote --url https://example.com/mcp`
- **THEN** the server is persisted with transport `http` and the given URL

#### Scenario: Removal hot-reloads a running agent

- **WHEN** the user runs `onclaw mcp remove fs` while an agent process is running
- **THEN** the running process is signaled and the server's tools are absent on the next run

### Requirement: MCP tools are surfaced through the existing tool layer

The system SHALL, at agent assembly, load enabled MCP servers, open one client per server
(transport-dispatched), initialize each, and aggregate the servers' tools into the agent's
tool set. MCP tools SHALL pass through the same redaction decorator and the agent's `tools`
allowlist filter as built-in tools. Agents with no MCP servers configured SHALL behave
exactly as before.

#### Scenario: MCP tools join the agent tool set

- **WHEN** an agent is assembled with an enabled MCP server exposing tool `search`
- **THEN** `search` is available to the agent subject to its `tools` allowlist

#### Scenario: No MCP servers leaves the agent unchanged

- **WHEN** an agent is assembled with no MCP servers configured
- **THEN** its tool set equals the built-in tools and assembly succeeds

### Requirement: A failing MCP server does not break the agent

The system SHALL isolate each MCP server's initialization and tool discovery behind a
per-server error boundary with an initialization timeout. A server that fails to start, fails
to initialize, or returns zero tools SHALL be logged and skipped; it SHALL NOT cause agent
assembly or the tool set of other servers to fail.

#### Scenario: One bad server is skipped

- **WHEN** two MCP servers are configured and one fails to initialize
- **THEN** the failing server is logged and skipped and the other server's tools remain available

### Requirement: The two standard transports are supported with a legacy fallback

The system SHALL support the `stdio` transport (launching the server as a subprocess) and the
`http` transport (Streamable HTTP, the current MCP standard). The system MAY support the
`sse` transport (the deprecated 2024-11-05 HTTP+SSE transport) as a fallback for older
servers. Each transport SHALL open, initialize, and close its client correctly.

#### Scenario: stdio transport launches a subprocess

- **WHEN** a server with transport `stdio` is enabled
- **THEN** the system launches its command as a subprocess, initializes the MCP session, and closes it on shutdown

#### Scenario: Streamable HTTP transport connects remotely

- **WHEN** a server with transport `http` is enabled
- **THEN** the system connects to its URL via Streamable HTTP, initializes the session, and closes it on shutdown

### Requirement: MCP server environment values are not leaked

The system SHALL mask MCP server environment values in `onclaw mcp list` output and in logs.
Plaintext storage of secret-bearing environment values in the `mcp_servers` table is a known
v1 limitation; resolution through the encrypted `SecretStore` is deferred.

#### Scenario: list output redacts env values

- **WHEN** the user runs `onclaw mcp list` for a server with configured environment values
- **THEN** the values are masked in the output