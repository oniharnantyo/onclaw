## ADDED Requirements

### Requirement: MCP servers are selectable per agent

The system SHALL associate MCP servers to agents through a many-to-many relation, and SHALL assemble an agent only with the tools exposed by its associated, enabled servers. A server associated with an agent SHALL contribute its tools to that agent subject to the agent's tool allowlist; a server not associated with an agent SHALL contribute no tools to that agent. The system SHALL provide a means to read and set the set of MCP servers associated with an agent.

#### Scenario: An agent only receives tools from its associated servers

- **WHEN** agent `coder` is associated with server `fs` and not with server `db`
- **THEN** the `coder` agent's tool set includes `fs`'s tools and excludes `db`'s tools

#### Scenario: Existing agents keep their servers after upgrade

- **WHEN** the per-agent association is introduced on a system that already has enabled MCP servers and agents
- **THEN** every previously enabled server is associated with every existing agent, so no agent loses tools it had before

## MODIFIED Requirements

### Requirement: MCP tools are surfaced through the existing tool layer

The system SHALL, at agent assembly, load the MCP servers associated with the agent, open one client per server (transport-dispatched), initialize each, and aggregate the associated servers' tools into the agent's tool set. MCP tools SHALL pass through the same redaction decorator and the agent's `tools` allowlist filter as built-in tools. Agents with no MCP servers associated SHALL behave exactly as before. A failing server SHALL be isolated behind a per-server error boundary and skipped without breaking assembly.

#### Scenario: MCP tools join the agent tool set when associated

- **WHEN** an agent is assembled with an associated, enabled MCP server exposing tool `search`
- **THEN** `search` is available to the agent subject to its `tools` allowlist

#### Scenario: An unassociated server contributes nothing

- **WHEN** an enabled MCP server is not associated with the agent being assembled
- **THEN** none of that server's tools are available to the agent