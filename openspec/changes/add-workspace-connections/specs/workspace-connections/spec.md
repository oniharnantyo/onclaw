## Purpose

Lets a workspace connect external services (GitHub, Jira, Figma, …) through a one-press Integrate flow: a per-service recipe gallery in workspace settings that turns a pasted access token into a workspace-scoped connection backed by the existing MCP runtime, so agents can attach and work the service without per-user authorization.

## ADDED Requirements

### Requirement: Service recipe registry

The system SHALL maintain a server-side registry of integration recipes, one per supported service, each declaring the service identity (id, name, icon), auth kind, transport endpoint, guided token-creation steps with recommended scopes, supported access levels, and availability (available or coming-soon). The recipes endpoint SHALL return the registry so clients can render service cards without hardcoding service knowledge.

#### Scenario: Listing recipes

- **WHEN** a workspace member requests the integration recipes
- **THEN** the response contains one entry per registered service, including availability and, for PAT services, the guided setup steps and recommended scopes

#### Scenario: OAuth-only service marked coming-soon

- **WHEN** a recipe's auth kind requires a flow that is not yet supported (e.g., OAuth)
- **THEN** the recipe is returned with availability "coming_soon" and no connect flow is offered for it

### Requirement: Integrations gallery

Workspace settings SHALL provide an Integrations surface that renders one card per registered service — available cards with an Integrate action, coming-soon cards rendered disabled with a coming-soon state — plus a Custom MCP server card that leads to the existing MCP server management, and a Connected section listing the workspace's connections with status, access level, attached agents, and manage/disconnect actions.

#### Scenario: Gallery shows services and connections

- **WHEN** a workspace member opens the Integrations settings surface
- **THEN** available services show Integrate buttons, coming-soon services are visible but not actionable, and already-connected services appear in the Connected section with their live status

#### Scenario: Connected service card state

- **WHEN** a connection's last probe succeeded
- **THEN** its card shows the connected status, the chosen access level, the agents it is attached to, and the token's last-4 hint — never the token itself

### Requirement: Connect a service with an access token

The system SHALL let an authorized user connect a PAT-auth service by submitting the recipe id, an access level, and the service's access token. The token SHALL be stored only as an encrypted secret header row bound to the workspace, the connection SHALL be created only if the recipe's probe succeeds against the service, and the system SHALL reject creating a second connection for a service the workspace already connected.

#### Scenario: Successful connect

- **WHEN** a user submits a valid GitHub token with access level read-only
- **THEN** the system runs the GitHub recipe's probe, on success stores the token as an encrypted secret header, creates the connection, and returns it with a connected status and the token's last-4 hint

#### Scenario: Probe failure blocks connect

- **WHEN** the submitted token fails the recipe's probe
- **THEN** no connection is created, nothing is stored, and the user receives the probe failure so they can correct the token

#### Scenario: Duplicate service connection rejected

- **WHEN** the workspace already has a connection for the requested service
- **THEN** the connect attempt is rejected with a conflict error identifying the existing connection

#### Scenario: Unknown recipe rejected

- **WHEN** a connect attempt references a recipe id that is not registered
- **THEN** the system rejects the request with a validation error

### Requirement: Connection materializes a workspace MCP server

Creating a connection SHALL create an ordinary workspace MCP server linked to the connection by an origin marker — streamable HTTP transport, the recipe's endpoint URL, and the secret header row — so the connection's tools reach agents through the existing MCP runtime unchanged. The materialized server SHALL participate in per-agent attachment, tool naming, policy, and status like any other workspace MCP server.

#### Scenario: Materialized server is attachable

- **WHEN** a connection is created
- **THEN** the linked MCP server appears in the agent MCP server list and an agent can attach to it through the existing attachment mechanism

#### Scenario: Materialized server tools work in a run

- **WHEN** an attached agent runs with the connection attached
- **THEN** the recipe's service tools are available under the provider-safe MCP naming, with credentials injected at connection time only

### Requirement: Connection probe and status

The system SHALL support probing a connection on demand using its recipe's probe action, persist the last probe result on the connection, and surface the status (including the failure message) to clients.

#### Scenario: Refresh status

- **WHEN** an authorized user triggers a probe on a connected service
- **THEN** the system re-runs the recipe's probe and the connection's status and status error are updated accordingly

### Requirement: Disconnect cascades

Disconnecting a connection SHALL remove the connection, its materialized workspace MCP server, and the server's per-agent attachment references, and SHALL leave the token unrecoverable.

#### Scenario: Disconnect removes all traces

- **WHEN** an authorized user disconnects a GitHub connection that two agents had attached
- **THEN** the connection and its linked MCP server are gone, no agent lists it among its servers, and the previously stored token cannot be retrieved

### Requirement: Access level

The system SHALL record an access level on every connection (read-only or read-write), default to read-only in the connect flow, surface it on the gallery and connection views, and drive the guided token scopes shown during connect. Access-level enforcement v1 rides the token's own scopes; the system SHALL NOT claim OnClaw-side action gating beyond what the service enforces.

#### Scenario: Read-only default

- **WHEN** a user opens the connect flow for a PAT service
- **THEN** the access level is preselected to read-only with the recipe's recommended read-only scopes displayed

#### Scenario: Access level recorded and shown

- **WHEN** a connection is created with access level read-write
- **THEN** the connection view and gallery card show "Read & write"

### Requirement: Connection secret handling

Connection tokens SHALL be encrypted at rest with the workspace as the authenticated-data binding, SHALL never be returned in full by any read path (last-4 hints only), and SHALL be resolved to plaintext only at MCP connection time. Token replacement SHALL follow the merge-on-name semantics used by MCP secret rows: an empty supplied value keeps the stored token.

#### Scenario: Token never readable

- **WHEN** any client reads the connection or its materialized MCP server
- **THEN** the token appears only as a last-4 hint

### Requirement: Managed server immutability

A workspace MCP server carrying an origin connection marker SHALL be read-plus-probe only through the MCP server surfaces: listing, status, tool count, and probe SHALL work as for any server, but edit and delete SHALL be rejected with an error that names the owning connection and points to the Integrations surface, where manage and disconnect live. Servers without an origin marker SHALL remain fully editable as before. The connection remains the single authority over a managed server's URL, secret rows, and lifecycle.

#### Scenario: Edit redirected to the connection

- **WHEN** an admin attempts to change the URL or headers of the GitHub connection's materialized server from MCP server management
- **THEN** the request is rejected with an error naming the GitHub connection and directing to the Integrations surface

#### Scenario: Delete replaced by disconnect

- **WHEN** an admin attempts to delete a materialized server from MCP server management
- **THEN** the request is rejected with the same pointer, and disconnecting the connection in Integrations removes it

#### Scenario: Probe and status still work

- **WHEN** an admin triggers a probe on a materialized server from MCP server management
- **THEN** the probe runs and the refreshed status is visible on both the MCP list and the Integrations gallery

#### Scenario: Hand-made servers unchanged

- **WHEN** an admin edits or deletes a workspace MCP server with no origin marker
- **THEN** the request behaves exactly as before this capability existed

### Requirement: Connection authorization

Viewing the recipes, gallery, and connections SHALL ride workspace membership. Creating, probing, or disconnecting connections SHALL require the dedicated `integrations.write` permission, whose default holders are exactly the built-in Owner, Admin, and Superadmin roles — Member SHALL never hold it, and custom roles SHALL hold it only when it is explicitly granted. Users without the permission SHALL see the gallery read-only.

#### Scenario: Member without manage permission

- **WHEN** a user holding only read permissions attempts to disconnect a connection
- **THEN** the request is rejected with a permission error

#### Scenario: Custom role with tool management still cannot connect

- **WHEN** a custom role holding the tool-settings write permission but not `integrations.write` attempts to connect a service
- **THEN** the request is rejected with a permission error

#### Scenario: Default admin roles connect

- **WHEN** a user with the built-in Owner, Admin, or Superadmin role connects a service
- **THEN** the connect proceeds subject to the connect flow's own gates (probe, duplicates, recipe availability)
