# workspace-mcp Specification

## Purpose

Lets workspaces register MCP servers — shared, admin-managed registry entries — and agents subscribe to them opt-in or carry their own private servers, turning external MCP tool ecosystems into agent capabilities with workspace-scoped secret handling.

## Requirements

### Requirement: Workspace MCP registry
A workspace MAY register MCP servers. Each registered server SHALL belong to exactly one workspace and carry: a name (unique per workspace, case-insensitive), a transport (`stdio`, `streamable_http`, or `sse`), a connection configuration validated per transport — stdio requires a command with optional args and optional environment variables; streamable HTTP and SSE require a URL with optional headers — and an enabled master switch (default enabled). Updating a server SHALL replace its editable fields. Deleting a server SHALL remove it; agent references to a deleted server SHALL become inert, not errors.

#### Scenario: Register a stdio server
- **WHEN** an admin creates a server named "GitHub" with transport `stdio`, command `npx`, args `[-y, @modelcontextprotocol/server-github]`, and one env var
- **THEN** response is 201; the server exists enabled with its config stored and no credentials echoed back

#### Scenario: Register a streamable HTTP server
- **WHEN** an admin creates a server with transport `streamable_http`, a URL, and an Authorization header
- **THEN** response is 201; the header value is stored encrypted and the response shows only a hint for it

#### Scenario: Duplicate name rejected
- **WHEN** a create or rename submits a name already used by another server in the same workspace (case-insensitive)
- **THEN** the save is rejected with a fielded validation error naming the conflict

#### Scenario: Transport config validated per transport
- **WHEN** a create submits transport `stdio` without a command, or transport `streamable_http` without a URL
- **THEN** response is 422 with per-field errors

#### Scenario: Master switch pauses a server
- **WHEN** a server is updated with `enabled: false`
- **THEN** no agent's executions expose its tools while paused, regardless of agent opt-in

#### Scenario: Delete leaves references inert
- **WHEN** a server referenced in some agents' `enabled_mcps` is deleted
- **THEN** the delete succeeds; those agents keep the stale id but it contributes no tools and raises no error

### Requirement: MCP credential secrecy
Environment variable values and header values SHALL be treated as secrets: encrypted at rest with the workspace-scoped key, never returned in plain text by any read (each is replaced by a non-secret hint), and replaceable only by supplying a new non-empty value — an empty value on update SHALL keep the stored secret. Values supplied as already-encrypted envelopes SHALL pass through untouched. Commands, args, URLs, and env/header names SHALL be stored in plain text.

#### Scenario: Secret never echoed
- **WHEN** any list or get returns a server carrying env var or header secrets
- **THEN** each secret value appears only as a hint; no plaintext or ciphertext is present in the response

#### Scenario: Empty value keeps the stored secret
- **WHEN** an update supplies an env var row with a known name and an empty value
- **THEN** the stored secret for that row is kept unchanged

#### Scenario: New value replaces the stored secret
- **WHEN** an update supplies a non-empty value for an env var row
- **THEN** the value is encrypted, stored, and a fresh hint is recorded

### Requirement: MCP API and permission gating
Workspace MCP servers SHALL be managed through workspace-scoped endpoints supporting list, get, create, update, delete, and explicit re-probe. Reads require `tools.read`; create, update, delete, and re-probe require `tools.write`. Non-members and unknown server ids SHALL receive 404 indistinguishably. Invalid configurations SHALL be rejected with fielded validation errors. Agent-private servers SHALL be managed through agent-scoped endpoints requiring `agents.write`, with the same configuration and secrecy rules.

#### Scenario: Member reads the registry
- **WHEN** a Member-role holder lists the workspace's MCP servers
- **THEN** response is 200 with the servers, their status, and tool counts (tools.read is granted to every built-in role)

#### Scenario: Member cannot configure
- **WHEN** a Member-role holder creates, updates, deletes, or re-probes a server
- **THEN** response is 403

#### Scenario: Cross-tenant server is not found
- **WHEN** a member of workspace A requests a server id belonging to workspace B
- **THEN** response is 404, indistinguishable from an unknown id

#### Scenario: Private servers require agent edit rights
- **WHEN** a holder of `agents.write` adds, edits, or removes a private server on an agent they can edit
- **THEN** the action succeeds; a holder without `agents.write` receives 403

### Requirement: Connection probe and status
Creating or updating a server SHALL attempt a connection and a tool listing; the response SHALL carry the resulting status (`connected` or `error`), the exposed-tool count, and on failure an error message. A re-probe SHALL be available on demand and behave identically. Status SHALL persist until the next probe or the next runtime connection attempt; the system SHALL NOT run background health checks, so status MAY be stale between probes.

#### Scenario: Successful probe
- **WHEN** a server is created pointing at a reachable MCP server exposing 24 tools
- **THEN** the response reports status `connected` with a tool count of 24

#### Scenario: Unreachable server
- **WHEN** a server is created pointing at an unreachable endpoint or failing command
- **THEN** the create still succeeds; the response reports status `error` with the connection failure message

#### Scenario: Re-probe on demand
- **WHEN** the holder of `tools.write` requests a re-probe of an errored server that has become reachable
- **THEN** the response reports status `connected` with the current tool count

### Requirement: Agent opt-in selection
An agent SHALL be exposed a workspace MCP server's tools only when the server is enabled AND the server's id appears in the agent's `enabled_mcps` allowlist. Selection SHALL be at server granularity — an agent takes a whole server's tool set or none of it. Newly registered workspace servers SHALL default to OFF for every agent; no agent gains tools until it subscribes.

#### Scenario: No opt-in exposes nothing
- **WHEN** an agent's `enabled_mcps` is empty and the workspace has registered servers
- **THEN** the agent's executions expose no workspace MCP tools

#### Scenario: Opt-in exposes the whole server
- **WHEN** an agent's `enabled_mcps` contains a connected, enabled server's id
- **THEN** the agent's executions expose that server's full tool set

#### Scenario: Paused server wins over opt-in
- **WHEN** an agent has opted into a server that the workspace has since paused
- **THEN** the agent's executions expose none of that server's tools

### Requirement: Agent-private MCP servers
An agent MAY carry private MCP servers, addressed and usable only by that agent. They SHALL use the same connection configuration, transports, and secrecy rules as workspace servers, with names unique per agent. They SHALL NOT appear in the workspace MCP registry or pane. Deleting the agent SHALL delete its private servers.

#### Scenario: Private server usable only by its agent
- **WHEN** agent A carries a private server and agent B executes in the same workspace
- **THEN** agent A's executions expose its tools and agent B's expose none of them

#### Scenario: Private servers die with the agent
- **WHEN** an agent carrying private servers is deleted
- **THEN** its private servers are removed and no orphan rows remain

#### Scenario: Private server config matches workspace rules
- **WHEN** a private server is created with a header secret
- **THEN** the secret is encrypted, hinted on read, and kept on empty update exactly like a workspace server's

### Requirement: stdio child environment is constructed, not inherited
The system SHALL launch stdio MCP server processes with a constructed environment consisting of (a) a fixed safe baseline and (b) exactly the connection's configured env rows, and SHALL NOT pass the OnClaw server process's other environment variables to the child. The baseline SHALL include at minimum `PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`, `TZ`, and the standard proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` and their lowercase forms). A configured env row MUST override a baseline entry with the same name. Probe dials and run dials SHALL construct the child environment identically. URL-transport (streamable HTTP, SSE) connections are unaffected by this requirement.

#### Scenario: Child sees baseline plus configured rows
- **WHEN** a stdio MCP server with env rows `FOO=bar` and `PATH=/custom/bin` is dialed for a run
- **THEN** the child process environment contains `FOO=bar`, `PATH=/custom/bin` (configured row wins over baseline), and the baseline variables (e.g. `HOME`, `HTTPS_PROXY`) when set on the host

#### Scenario: Parent-only variables are not leaked
- **WHEN** the OnClaw server process has a variable `DATABASE_URL` that is not in the MCP connection's configured env rows
- **THEN** the stdio child process environment does not contain `DATABASE_URL`, whether dialed by a run or by a probe

#### Scenario: Probe and run environments match
- **WHEN** the same stdio MCP server is dialed once by a probe and once by an agent run with identical configuration
- **THEN** the child observes the same environment variable set in both dials

#### Scenario: URL transports unchanged
- **WHEN** a streamable HTTP or SSE MCP connection is dialed
- **THEN** only the configured header rows are attached to requests and the parent environment plays no role
