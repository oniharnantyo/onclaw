# workspace-connections Specification

## Purpose

Lets a workspace connect external services (GitHub, Jira, Figma, …) through a one-press Integrate flow: a per-service recipe gallery in workspace settings that turns a pasted access token into a workspace-scoped connection backed by the existing MCP runtime, so agents can attach and work the service without per-user authorization.

## Requirements

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

### Requirement: OAuth connect flow

For recipes declaring auth kind `oauth`, the connect action SHALL start an authorization-code flow: the system SHALL return an authorize redirect URL built from the recipe's authorize endpoint and the provider's registered app, carry a single-use signed state binding the workspace and the connect attempt, and — after provider consent — exchange the code at the recipe's token endpoint, run the recipe's probe with the obtained token, and activate the connection only on probe success. On probe failure the exchanged tokens SHALL be discarded and no connection SHALL be created. If the provider has no registered instance app, the connect attempt SHALL fail with an error that names the missing registration.

#### Scenario: Successful OAuth connect

- **WHEN** a user completes provider consent for an OAuth recipe with a registered app and the exchanged token passes the probe
- **THEN** the connection is created with a connected status, the token set is stored encrypted, and the materialized MCP server's `Authorization` row holds the access token

#### Scenario: Probe failure discards tokens

- **WHEN** the exchanged token fails the recipe's probe
- **THEN** the token set is discarded, no connection and no materialized server exist, and the user sees the probe failure

#### Scenario: Missing app registration

- **WHEN** a user presses Integrate on an OAuth recipe whose provider has no registered instance app
- **THEN** the attempt fails with an error naming the required instance-level registration, and the card is not silently broken

### Requirement: Token refresh lifecycle

The system SHALL store the refresh token, expiry, and granted scopes encrypted on the connection, and SHALL refresh the access token — using the recipe's token endpoint — when a credential resolution occurs within the recipe's refresh margin. A successful refresh SHALL write the new access token into the connection's materialized server `Authorization` secret row through the existing secret machinery; the MCP runtime SHALL require no changes. A failed refresh SHALL mark the connection `expired`, surface that status with the provider error, and leave prior tool calls unaffected.

#### Scenario: Refresh within margin

- **WHEN** an attached agent's run resolves the connection's credentials and the access token is inside the refresh margin
- **THEN** the system refreshes the token, the run proceeds with the new token, and the stored expiry advances

#### Scenario: Failed refresh expires the connection

- **WHEN** a refresh attempt fails (revoked, network, provider rejection)
- **THEN** the connection's status becomes `expired` with the provider error surfaced, and subsequent runs degrade as they do for any errored MCP server rather than failing the run host

### Requirement: Reauthorization

An expired OAuth connection SHALL offer a Reauthorize action that re-runs the consent flow for the same connection: on success the token set is replaced in place, the status returns to connected, and the connection id, materialized server, and agent attachments are preserved.

#### Scenario: Reauthorize an expired connection

- **WHEN** an authorized user reauthorizes an expired Atlassian connection
- **THEN** a new consent flow completes, the token set is replaced, the status returns to connected, and agents attached before the expiry remain attached

### Requirement: OAuth recipe availability

A recipe with auth kind `oauth` SHALL render its gallery card as available only when the provider's instance app is registered; otherwise it SHALL remain a coming-soon card. Coming-soon-to-available transitions SHALL require no code change — only the recipe's declaration and the app registration.

#### Scenario: Card activates with registration

- **WHEN** an instance admin registers the Atlassian app while the recipe already declares auth kind oauth
- **THEN** the Atlassian card renders as available with the Integrate action on next gallery load

### Requirement: Connection attachment management

An authorized user SHALL set the complete set of agents attached to a connection from the connection itself: the request names the desired agent set, the system computes the difference against the current attachment, and exactly the affected agents change — either every affected agent's allowlist is updated or none is. Attachment applied from the connection side SHALL be indistinguishable from attachment applied in the agent's configuration: it stores the connection's materialized server id for MCP-kind connections and the connection's own id for http-kind connections. Requests naming an agent that does not exist in the workspace SHALL be rejected with a validation error and change nothing.

#### Scenario: Attaching several agents in one save

- **WHEN** an authorized user saves a connection's edit surface with two agents selected and one deselected
- **THEN** exactly the two selected agents' allowlists gain the connection and the deselected agent's loses it in one atomic update, and the connection view lists the new attached agents

#### Scenario: Http-kind attachment stores the connection id

- **WHEN** an agent is attached to an http-kind connection
- **THEN** the agent's allowlist carries the connection's own id, and the connection's declared verbs become that agent's tools on its next run

#### Scenario: Unknown agent rejected atomically

- **WHEN** an attachment update names an agent id that does not exist in the workspace
- **THEN** the request is rejected with a validation error and no agent's attachment changes

#### Scenario: Member cannot manage attachment

- **WHEN** a user without `integrations.write` attempts an attachment update
- **THEN** the request is rejected with a permission error

### Requirement: Connect completion offers agent selection

After a connection activates, the connect flow SHALL present the workspace's agents for attachment regardless of auth kind: token-auth connects present the selection as the completion step of the connect dialog, and OAuth connects present it when the consent callback activates the connection. Skipping the selection SHALL be allowed — the connection stays active with no agents attached, and attachment remains available from the connection's edit surface.

#### Scenario: Token connect completion lists agents

- **WHEN** a token-auth connect succeeds and the workspace holds multiple agents
- **THEN** the completion step lists every agent with an independent toggle, and the toggles saved there take effect as ordinary attachment

#### Scenario: OAuth callback success lands on selection

- **WHEN** an OAuth connect returns to the integrations surface with a success status
- **THEN** the activation lands on the new connection's agent-selection surface without repeating the connect flow

#### Scenario: Skipping selection attaches no one

- **WHEN** the user dismisses the completion selection without toggling any agent
- **THEN** the connection remains active with no agents attached and no error

### Requirement: Connection token replacement

Token-auth and http-kind connections SHALL accept a replacement access token from an authorized user: the system SHALL verify the candidate token with the recipe's probe before storing, replace the stored token in place only on success, and preserve the connection's identity, origin, access level, and every agent attachment. A failed probe SHALL keep the previously stored token unchanged and surface the probe failure. An empty submission SHALL leave the stored token untouched. OAuth-kind connections SHALL NOT accept token replacement — reauthorization is their credential-rotation path.

#### Scenario: Replacement swaps the token in place

- **WHEN** an authorized user submits a valid replacement token for a connected service
- **THEN** the stored token is replaced, subsequent calls authenticate with the new token, the last-4 hint updates, and every attached agent keeps its tools without re-attachment

#### Scenario: Failed probe keeps the stored token

- **WHEN** the submitted replacement token fails the recipe's probe
- **THEN** nothing is stored, the previously stored token keeps authenticating, and the probe failure is surfaced so the user can correct the token

#### Scenario: Empty submission changes nothing

- **WHEN** an edit is saved with the token field left empty
- **THEN** the stored token is kept unchanged and the save still applies any attachment changes

#### Scenario: OAuth-kind replacement refused

- **WHEN** a token replacement is requested for an OAuth-kind connection
- **THEN** the request is rejected with an error directing to reauthorization

#### Scenario: Member cannot replace a token

- **WHEN** a user without `integrations.write` attempts a token replacement
- **THEN** the request is rejected with a permission error
