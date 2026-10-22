# Spec Delta

## MODIFIED Requirements

### Requirement: MCP pane

The settings menu SHALL let the user add, edit, and remove MCP server definitions (transport, command or URL, arguments, environment) and SHALL show each server's live connection state — probed on demand, showing connected with the discovered tool count, failed with the reason, or authorization required. Every enabled server is available to agents by default; agents restrict the set from their own configuration. Each server row SHALL show an enabled toggle, a colored status dot (success for connected, danger for failed, warning for authorization-required, neutral for un-probed) alongside the status chip, and actions to refresh and remove the server; the pane SHALL offer an action that refreshes every enabled server. HTTP and SSE server definitions SHALL offer an Authentication setting of none (static headers) or OAuth. A row in the authorization-required state SHALL offer an Authorize action that starts the authorization flow, and an OAuth-authenticated row SHALL offer Disconnect. Disabling a server SHALL take effect immediately; enabling it SHALL refresh the row.

#### Scenario: Add an MCP server

- **WHEN** the user adds a valid MCP server definition
- **THEN** the server is listed with its connection state and becomes available to every agent that has not restricted its server list

#### Scenario: Row toggle disables and enables

- **WHEN** the user toggles a server row off
- **THEN** the server's live connection is dropped immediately and agents no longer receive its tools, and the row shows its disabled state

- **WHEN** the user toggles a server row on
- **THEN** the row refreshes the server so its connection state becomes visible

#### Scenario: Status dot reflects connection state

- **WHEN** a server row is shown in either theme
- **THEN** the row's dot is green when connected, red when failed, amber when authorization is required, and neutral when un-probed

#### Scenario: Refresh all re-dials every enabled server

- **WHEN** the user invokes the refresh-all action
- **THEN** every enabled server is refreshed and each row updates with its fresh outcome

#### Scenario: Authorize action starts the flow

- **WHEN** the user invokes Authorize on a row in the authorization-required state
- **THEN** the system browser opens the server's authorization page, the row is unaffected until the flow completes, and on completion the row refreshes to the new state

#### Scenario: Authentication setting on HTTP servers

- **WHEN** the user edits an HTTP or SSE server definition
- **THEN** the dialog offers Authentication with none and OAuth choices, and the OAuth choice authorizes through the flow rather than static headers

#### Scenario: Disconnect returns the row to authorization-required

- **WHEN** the user invokes Disconnect on an OAuth-authenticated server
- **THEN** its stored tokens are revoked and cleared and the row shows the authorization-required state with an Authorize action

#### Scenario: Probe shows live state

- **WHEN** the user probes a reachable server
- **THEN** its row shows connected with the discovered tool count

#### Scenario: Failed probe shows the reason

- **WHEN** the user probes a server that cannot be reached
- **THEN** its row shows the failed state with the underlying reason and the stored definition is unchanged
