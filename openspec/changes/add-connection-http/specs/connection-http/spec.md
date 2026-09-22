## Purpose

Lets agents call REST APIs of services that have no cloud-reachable MCP server (Figma's server is desktop-local) through connection-scoped request tools: one tool per connection, pinned to a recipe-declared base URL, with the connection's credential injected server-side so tokens never enter model context.

## ADDED Requirements

### Requirement: HTTP-kind connections

A recipe MAY declare the connection kind `http` with a base URL, an auth header name, a probe call, and its declared verb tools. Connecting an HTTP-kind recipe SHALL create a connection with the same lifecycle as other connections — workspace-scoped secrets, access level, one-per-service uniqueness, probe-gated activation, disconnect cascade — except that no MCP server is materialized; instead the connection contributes its declared verb tools directly to agent tool resolution.

#### Scenario: Connect a Figma connection

- **WHEN** a user connects the Figma recipe with a valid token and the probe call succeeds
- **THEN** the connection exists with a connected status, no workspace MCP server row is created for it, and the declared verb tools become available to agents that attach the connection

#### Scenario: Disconnect cascades like any connection

- **WHEN** an authorized user disconnects an HTTP-kind connection
- **THEN** the connection is removed, its declared verb tools stop resolving for all agents, and the stored token is unrecoverable

### Requirement: Recipe-declared verb tools

The recipe SHALL declare an HTTP-kind connection's entire tool surface as data: each verb tool names an HTTP method, a path template, typed parameters, and a description, exposed to agents under the service-prefixed name (e.g., `figma.get_comments`). Invocation SHALL pre-bind the declared method and path, bind and validate parameter values — encoded, rejecting path traversal and absolute-URL values — join to the recipe-declared base URL, and re-validate the resulting URL before any network request. **No free-form request tool SHALL exist**: agents can call only the verbs the recipe declares, and covering a new operation requires a recipe release. Off-host redirects SHALL be refused, and the connection's auth header SHALL be attached at call time from its decrypted secret; tool schemas and descriptions SHALL contain no credential material.

#### Scenario: Declared verb invocation

- **WHEN** an agent calls `figma.get_comments` with a file key on an attached connection
- **THEN** the request goes to the base URL with the declared method and bound path, the auth header attached server-side, and the JSON response returns size-capped into the transcript

#### Scenario: Undeclared operation is impossible

- **WHEN** the agent needs an operation the recipe does not declare
- **THEN** no tool exists for it; the exposed surface is exactly the declared verb list

#### Scenario: Parameter injection rejected

- **WHEN** a bound parameter value attempts path traversal or carries an absolute URL
- **THEN** the call is rejected before any network request is made

#### Scenario: Redirect off-host not followed

- **WHEN** the service responds with a redirect to a different host
- **THEN** the tool returns an error instead of following it

### Requirement: Connection tool source

The runner's tool resolution SHALL consult HTTP-kind connections of the agent's attached workspace connections and yield one tool per declared verb after built-ins, in the same pass that resolves MCP tools and under the same degradation rule: a connection whose credentials fail to resolve SHALL have its tools skipped and marked, never failing the run.

#### Scenario: Attached connection's tools resolve

- **WHEN** an agent with an attached Figma connection starts a run
- **THEN** the connection's declared verb tools appear among the run's tools after built-ins

#### Scenario: Unresolvable credential degrades

- **WHEN** an HTTP-kind connection's token cannot be decrypted at resolution
- **THEN** the run proceeds without the connection's tools, the skip is marked in run diagnostics, and the run does not fail

### Requirement: HTTP probe and status

The recipe's probe call — a cheap read request against the base URL — SHALL gate connect exactly as the MCP probe does, and its result SHALL persist as the connection's status, refreshable on demand.

#### Scenario: Probe gates connect

- **WHEN** a submitted Figma token fails the recipe's probe call
- **THEN** no connection is created and nothing is stored
