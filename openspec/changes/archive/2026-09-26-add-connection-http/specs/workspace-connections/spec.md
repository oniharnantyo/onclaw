## MODIFIED Requirements

### Requirement: Connection materializes a workspace MCP server

A connection whose recipe declares kind `mcp` SHALL create an ordinary workspace MCP server linked to the connection by an origin marker — streamable HTTP transport, the recipe's endpoint URL, and the secret header row — so the connection's tools reach agents through the existing MCP runtime unchanged. A connection whose recipe declares kind `http` SHALL NOT materialize an MCP server; it contributes its request tool per the connection-http capability. Both kinds participate in per-agent attachment, status, and disconnect cascade; MCP-kind servers additionally participate in tool naming and MCP policy.

#### Scenario: Materialized server is attachable

- **WHEN** an MCP-kind connection is created
- **THEN** the linked MCP server appears in the agent MCP server list and an agent can attach to it through the existing attachment mechanism

#### Scenario: Materialized server tools work in a run

- **WHEN** an attached agent runs with the connection attached
- **THEN** the recipe's service tools are available under the provider-safe MCP naming, with credentials injected at connection time only

#### Scenario: HTTP-kind connection contributes a tool, not a server

- **WHEN** an HTTP-kind connection is created
- **THEN** no workspace MCP server row exists for it, and its request tool reaches attached agents through the connection tool source
