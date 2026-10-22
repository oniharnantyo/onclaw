# Spec Delta

## Purpose

Authenticate buddies to MCP servers that follow the MCP authorization specification: an OAuth 2.1 client flow with durable, self-refreshing credentials, a distinct authorization-required connection state, and refresh semantics that re-establish live connections.

## ADDED Requirements

### Requirement: OAuth server authentication

The app SHALL support OAuth 2.1 client authentication for HTTP and SSE MCP servers whose protection advertises OAuth via a `WWW-Authenticate` header naming a protected-resource metadata document. The app SHALL discover the authorization server from that document, register itself dynamically when it has no stored registration for the server, and authorize through the user's system browser with an authorization-code flow using PKCE and a loopback redirect. Obtained tokens SHALL persist across restarts and SHALL refresh automatically without user interaction. Each server SHALL support an explicit Authentication setting of none (static headers) or OAuth; the Authorization flow SHALL NOT block or extend the bounded connect and probe paths.

#### Scenario: First authorization connects the server

- **WHEN** the user authorizes an HTTP server that advertises OAuth and completes the browser flow
- **THEN** the tokens are stored and the server connects and reports connected with its discovered tools

#### Scenario: Expired tokens refresh headlessly

- **WHEN** a server with a stored, expired access token is used or refreshed after restart, including in server mode with no browser available
- **THEN** the client refreshes the token and connects without user interaction

#### Scenario: Failed refresh returns to authorization-required

- **WHEN** a stored refresh token is no longer accepted
- **THEN** the server reports the authorization-required state and does not report a generic failure

#### Scenario: Disconnect revokes and clears

- **WHEN** the user disconnects an OAuth-authenticated server
- **THEN** the stored tokens are deleted, the token is revoked at the advertised revocation endpoint when one exists, the registration is kept, and the server reports the authorization-required state

### Requirement: Authorization-required connection state

The probe SHALL distinguish servers that require user authorization from generic connection failures: a connection rejected as unauthenticated whose protection advertises OAuth metadata SHALL persist an authorization-required state, and any other failed connection SHALL persist the failure state with its reason. An authorization-required server SHALL NOT be treated as healthy by agents.

#### Scenario: Unauthenticated OAuth server probes as authorization-required

- **WHEN** a probe of a server advertising OAuth is rejected with an unauthenticated response and valid protected-resource metadata
- **THEN** the server persists the authorization-required state rather than the failure state

#### Scenario: Authorizing clears the state

- **WHEN** the authorization flow completes for a server in the authorization-required state
- **THEN** the server is re-probed and reports connected or, if it remains unreachable, the failure state with its reason

### Requirement: Connection refresh

Refreshing a server SHALL first discard its cached runtime connection and then re-dial the stored definition, so agents use fresh credentials immediately after a refresh rather than after a cache-retry or idle window. Refreshing all SHALL apply this to every enabled server. The stored definition SHALL NOT be altered by a refresh.

#### Scenario: Refresh swaps live credentials

- **WHEN** a user refreshes a server after its tokens changed
- **THEN** subsequent agent turns use a connection established with the current credentials, and the row shows the fresh outcome

#### Scenario: Refresh failure is shown, definition untouched

- **WHEN** a refresh cannot establish a connection
- **THEN** the row shows the failure or authorization-required outcome and the server definition is unchanged
