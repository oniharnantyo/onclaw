# agent-runtime Delta

## ADDED Requirements

### Requirement: MCP tool resolution
An agent's execution SHALL expose MCP tools in addition to the built-in surface resolved from its `tools` allowlist: the tools of every workspace MCP server whose id appears in the agent's `enabled_mcps` while that server is enabled at the workspace level, plus the tools of the agent's private MCP servers. MCP tool exposure SHALL be independent of the `tools` allowlist, per-turn allowed-tools overrides, and the workspace tool gate — it is governed solely by the opt-in allowlist, the workspace master switch (see the workspace-mcp capability), and private server attachment. MCP tools SHALL surface under names of the form `mcp__<server>__<tool>` sanitized to provider-safe characters; a name collision after sanitization SHALL be resolved by suffixing so every tool remains individually addressable. A server that cannot be contacted or fails tool listing at execution SHALL contribute no tools, SHALL have its stored status moved to `error` with the failure message, and SHALL NOT fail the run. Tool selection SHALL be at server granularity — no per-tool filtering within a server.

#### Scenario: Opted-in server contributes tools
- **WHEN** an agent's `enabled_mcps` contains a connected, enabled server named "github" exposing `create_issue`
- **THEN** the agent's executions include a tool addressable as `mcp__github__create_issue` alongside its built-in tools

#### Scenario: Empty opt-in exposes nothing
- **WHEN** an agent's `enabled_mcps` is empty and it has no private servers
- **THEN** its executions expose no MCP tools, regardless of what the workspace has registered

#### Scenario: Workspace master switch wins
- **WHEN** an agent has opted into a server that is paused at the workspace level
- **THEN** none of that server's tools appear in the agent's executions

#### Scenario: Private server appears only for its agent
- **WHEN** agent A carries a private server and both agents execute
- **THEN** only agent A's executions include that server's tools

#### Scenario: MCP tools ignore the tools allowlist
- **WHEN** an agent's `tools` array is empty but its `enabled_mcps` contains a connected server
- **THEN** its executions expose no built-in registry tools but do expose the server's MCP tools

#### Scenario: Dead server degrades gracefully
- **WHEN** an execution resolves tools and one opted-in server cannot be contacted
- **THEN** the run proceeds with the remaining tools, the server's stored status becomes `error` with the failure message, and no error surfaces to the conversation on that server's account

#### Scenario: Names survive sanitization
- **WHEN** a server named "My Server" exposes a tool `do.thing` and another server exposes a tool sanitizing to the same name
- **THEN** both tools are exposed with distinct, provider-safe `mcp__` names
