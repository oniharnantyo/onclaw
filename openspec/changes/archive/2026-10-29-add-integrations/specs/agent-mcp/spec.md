# Spec Delta

## MODIFIED Requirements

### Requirement: OAuth server authentication

The app SHALL support OAuth 2.1 client authentication for HTTP and SSE MCP servers whose protection advertises OAuth via a `WWW-Authenticate` header naming a protected-resource metadata document. The app SHALL discover the authorization server from that document, register itself dynamically when it has no stored registration for the server, and authorize through the user's system browser with an authorization-code flow using PKCE and a loopback redirect. When the server's stored definition instead carries a pre-registered client (client ID and secret), the app SHALL authorize through that client — skipping dynamic registration — while using the same browser flow, PKCE, and loopback redirect. Obtained tokens SHALL persist across restarts and SHALL refresh automatically without user interaction. Each server SHALL support an explicit Authentication setting of none (static headers) or OAuth; the Authorization flow SHALL NOT block or extend the bounded connect and probe paths.

#### Scenario: First authorization connects the server

- **WHEN** the user authorizes an HTTP server that advertises OAuth and completes the browser flow
- **THEN** the tokens are stored and the server connects and reports connected with its discovered tools

#### Scenario: Pre-registered client skips registration

- **WHEN** the user authorizes a server whose definition stores a pre-registered client ID and secret, and the protection's authorization server does not advertise dynamic registration
- **THEN** the authorization uses the stored client and completes the browser flow without attempting registration

#### Scenario: Expired tokens refresh headlessly

- **WHEN** a server with a stored, expired access token is used or refreshed after restart, including in server mode with no browser available
- **THEN** the client refreshes the token and connects without user interaction

#### Scenario: Failed refresh returns to authorization-required

- **WHEN** a stored refresh token is no longer accepted
- **THEN** the server reports the authorization-required state and does not report a generic failure

#### Scenario: Disconnect revokes and clears

- **WHEN** the user disconnects an OAuth-authenticated server
- **THEN** the stored tokens are deleted, the token is revoked at the advertised revocation endpoint when one exists, the registration is kept, and the server reports the authorization-required state
