# web-app/settings Delta

## MODIFIED Requirements

### Requirement: MCP servers pane
The MCP pane SHALL render from the workspace MCP endpoints (API-backed, no local mock state): one card per registered server showing its name, transport, status dot (Connected/Paused/Error), exposed-tool count, and the number and names of agents whose `enabled_mcps` reference it. Adding and editing SHALL happen through a structured dialog — never free-text config entry — with one labeled control per property: server name; transport select (`stdio`, `streamable_http`, `sse`); for stdio a command input, an args input, and an env-var row editor (name plus write-only value showing the stored hint); for streamable HTTP and SSE a URL input and a header row editor (name plus write-only value showing the stored hint). Submitting the dialog SHALL create or update via the API and surface the probe result — status and error message — so a misconfigured server is visible immediately. Pausing/resuming SHALL toggle the server's master switch via the API; errored servers SHALL offer a retry that triggers a re-probe; deletion SHALL ask for confirmation. Expanding a server SHALL list its exposed tool names as read-only chips. Destructive or write actions SHALL be limited to `tools.write` holders; read-only members see the pane without them.

#### Scenario: Pause with dependents
- **WHEN** a connected server referenced by two agents' `enabled_mcps` is paused
- **THEN** its status becomes Paused, its row dims, and the usage note flags the referencing agents as losing its tools

#### Scenario: Add server via dialog
- **WHEN** the user submits the add dialog for a `stdio` server — name "GitHub", command `npx`, args, and one env var
- **THEN** the server is created via the API, appears in the list with its probe status (Connected or Error with the message), and the dialog closes

#### Scenario: Add HTTP server with secret header
- **WHEN** the user adds a `streamable_http` server with an Authorization header value
- **THEN** the value is submitted once and the dialog afterwards shows only its stored hint — never the value

#### Scenario: Edit server
- **WHEN** the user opens edit on a server, changes its transport through the select (reconfiguring the form), and saves
- **THEN** the row shows the new transport and the fresh probe status after save

#### Scenario: Edit keeps the stored secret
- **WHEN** the user opens edit on a server and saves without retyping a header value
- **THEN** the update sends an empty value for that header and the stored secret is kept

#### Scenario: Transport select reconfigures the form
- **WHEN** the user switches the transport select from `stdio` to `streamable_http`
- **THEN** the command/args/env controls are replaced by URL and header controls

#### Scenario: Failed probe surfaces inline
- **WHEN** a create or edit probe cannot reach the server
- **THEN** the card's status dot turns Error with the failure message visible and no toast-only error

#### Scenario: Retry errored server
- **WHEN** the user clicks retry on an errored server
- **THEN** a re-probe is requested via the API and the status returns to Connected or stays Error with a refreshed message

#### Scenario: Delete asks for confirmation
- **WHEN** the user deletes a server referenced by agents
- **THEN** a confirmation names the referencing agents before the delete is sent

#### Scenario: Member sees a read-only pane
- **WHEN** a holder without `tools.write` opens the pane
- **THEN** server cards and tool lists render, but add/edit/pause/retry/delete controls are absent
