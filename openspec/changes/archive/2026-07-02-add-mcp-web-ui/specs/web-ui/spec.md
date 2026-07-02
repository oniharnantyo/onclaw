## ADDED Requirements

### Requirement: The web console provides an MCP servers management page

The web console SHALL include an MCP servers page, reachable from the shell navigation, that lists
every configured MCP server with its name, transport, and enabled state. The page SHALL let the
user add and edit servers through a modal whose fields adapt to the selected transport (stdio:
command, arguments, and environment variables; http/sse: URL), toggle a server between enabled and
disabled, remove a server, and test an unsaved configuration to preview its discovered tools before
saving. All page actions SHALL operate exclusively through the authenticated `/api/mcp` endpoints.

#### Scenario: The MCP page is reachable from navigation

- **WHEN** an authenticated user clicks the MCP Servers navigation item
- **THEN** the MCP servers page renders and lists the configured servers

#### Scenario: A server is added through the modal

- **WHEN** the user opens the add modal, selects transport `stdio`, fills in a command and args, and
  saves
- **THEN** the server is created via `POST /api/mcp` and appears enabled in the list

#### Scenario: An unsaved server is tested from the modal

- **WHEN** the user fills in a server configuration in the modal and clicks Test
- **THEN** the page calls `POST /api/mcp/test` and displays the discovered tool names without saving

#### Scenario: A server is toggled from the list

- **WHEN** the user clicks the enabled toggle on a server row
- **THEN** the page calls `POST /api/mcp/{name}/toggle` and the row's state updates
