## ADDED Requirements

### Requirement: MCP servers are managed via the authenticated web API

The system SHALL expose MCP server management over the same authenticated REST API as other
console resources. It SHALL provide endpoints to list (`GET /api/mcp`), read
(`GET /api/mcp/{name}`), create or upsert (`POST /api/mcp`), update (`PUT /api/mcp/{name}`),
remove (`DELETE /api/mcp/{name}`), and toggle enable/disable (`POST /api/mcp/{name}/toggle`) MCP
servers, all persisted through the existing `MCPServerStore`. A create/update SHALL validate that
the transport is one of `stdio`, `http`, or `sse`; that a stdio server has a command; and that an
`http`/`sse` server has a URL. Read responses (list and get) SHALL redact environment-variable
values (preserving their keys) so secrets are never returned in the clear. The API SHALL accept a
connection test (`POST /api/mcp/test`) that opens a client to the supplied (unsaved) configuration
and returns the discovered tool names, without persisting it.

#### Scenario: A stdio server is created through the API

- **WHEN** an authenticated `POST /api/mcp` is sent with name `fs`, transport `stdio`, command
  `npx`, and args
- **THEN** the server is persisted and the response echoes it with env values redacted

#### Scenario: An http server without a URL is rejected

- **WHEN** an authenticated `POST /api/mcp` is sent with transport `http` and no URL
- **THEN** the server responds 400 and nothing is persisted

#### Scenario: A server is toggled disabled then enabled

- **WHEN** an authenticated `POST /api/mcp/fs/toggle` is sent with `{enabled: false}` and then
  `{enabled: true}`
- **THEN** the server's enabled flag is updated to disabled and back to enabled

#### Scenario: An unsaved server is tested before saving

- **WHEN** an authenticated `POST /api/mcp/test` is sent with a stdio configuration exposing tool
  `search`
- **THEN** the response lists `search` and no server is persisted

#### Scenario: Read responses never expose env values

- **WHEN** an authenticated `GET /api/mcp` lists a server whose env contains `TOKEN=secret`
- **THEN** the env value is redacted (e.g. `TOKEN=***`) while the key is preserved

### Requirement: Web-API MCP mutations hot-reload the running agent

The MCP manager SHALL cache its discovered tool set for reuse across agent turns, and SHALL provide
a reload operation that drops that cache and closes its open clients so the next tool discovery
re-reads the current server definitions from the store. Any successful create, update, toggle, or
remove performed through the web API SHALL trigger this reload. As a result, a change made through
the console SHALL be visible to a chat assembled after the mutation, without restarting
`onclaw serve`.

#### Scenario: A server added in the console is usable on the next turn

- **WHEN** a server exposing tool `search` is added through the API while `serve` is running, and a
  chat turn is then assembled
- **THEN** `search` is available to that agent

#### Scenario: A removed server's tools disappear on the next turn

- **WHEN** a server is removed through the API after its tools were cached, and a chat turn is then
  assembled
- **THEN** that server's tools are no longer available to the agent

#### Scenario: Reload does not corrupt an in-flight discovery

- **WHEN** a reload runs concurrently with a tool discovery already in progress
- **THEN** the reload is serialized under the manager's lock and produces no torn tool set
