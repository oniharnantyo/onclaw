## Purpose

Defines OAuth 2.0 protection for MCP server connections: how OnClaw discovers a remote MCP server's authorization requirements, registers or presents itself as a client, obtains and stores tokens (browser and device flows), refreshes them on dial, and attaches them as bearer credentials — enabling connection to OAuth-only remote MCP servers (GitLab, Notion, Linear, Slack, Stripe, Asana, Sentry) without per-provider admin registration.

## ADDED Requirements

### Requirement: MCP connection auth mode
An MCP connection SHALL declare an auth mode: `none` (the default — configured static headers/rows only, behavior unchanged) or `oauth`. The `oauth` mode SHALL be valid only for URL transports (streamable HTTP, SSE). Static-header connections MUST continue to dial exactly as before.

#### Scenario: Static headers unchanged
- **WHEN** a server configured with auth mode `none` and static headers is dialed
- **THEN** requests carry only the configured header rows and no OAuth machinery runs

#### Scenario: OAuth mode on stdio rejected
- **WHEN** a server config sets auth mode `oauth` with the stdio transport
- **THEN** the configuration is rejected with a validation error

### Requirement: Protected-resource discovery
For an `oauth`-mode connection lacking a usable token, the system SHALL discover authorization metadata by requesting the server URL and, on a 401 with a `WWW-Authenticate` header carrying `resource_metadata`, fetching that metadata document; otherwise by probing the RFC 9728 well-known location derived from the server URL (including path-inserted well-knowns). From the protected-resource metadata the system SHALL select an authorization server and fetch its RFC 8414 metadata (authorization, token, and — where offered — registration and device endpoints, grant types, PKCE support, issuer).

#### Scenario: WWW-Authenticate driven discovery
- **WHEN** an OAuth-mode dial of `https://gitlab.com/api/v4/mcp` receives 401 with `resource_metadata="https://gitlab.com/.well-known/oauth-protected-resource/api/v4/mcp"`
- **THEN** the client fetches that document, resolves authorization server `https://gitlab.com`, and reads its OAuth metadata including the `mcp` scope requirement

#### Scenario: Well-known fallback
- **WHEN** a server does not return a parseable `WWW-Authenticate` challenge but publishes `/.well-known/oauth-protected-resource` at its URL
- **THEN** discovery succeeds from the well-known document

#### Scenario: Undiscoverable server
- **WHEN** neither the challenge nor the well-knowns yield protected-resource metadata
- **THEN** the connection is marked errored with a discovery-failure detail and no token request is attempted

### Requirement: Client registration strategies
For a discovered authorization server the client SHALL identify itself, in order: (a) **pre-registered app** when the server config carries client credentials; (b) **Client ID Metadata Document** when the configured or self-published client id is an HTTPS URL serving a valid client metadata document; (c) **dynamic client registration** when the authorization server advertises a `registration_endpoint`. The resulting client SHALL use PKCE with S256 for the authorization-code flow whenever the server advertises code-challenge support.

#### Scenario: DCR against a GitLab-class server
- **WHEN** an OAuth-mode GitLab connection with no pre-registered app connects and the authorization server advertises `registration_endpoint`
- **THEN** the client registers dynamically, receives client credentials, and proceeds to authorization with PKCE S256

#### Scenario: BYO app without DCR
- **WHEN** a server config carries a pre-registered client id (and secret, if confidential) and the authorization server does not advertise registration
- **THEN** authorization proceeds using the configured client, with the secret sent only to the token endpoint

#### Scenario: Registration refusal surfaced
- **WHEN** DCR is refused (e.g. rate limited or disabled) and no pre-registered app is configured
- **THEN** the connection is marked errored with the provider's error detail and guidance to configure a pre-registered app

### Requirement: Authorization flows
The system SHALL obtain tokens via the authorization-code flow: it mints a single-use, server-bound sealed state, redirects the user to the authorization endpoint with PKCE and the derived redirect URI, and completes on callback by exchanging the code. When the instance has no public base URL and the authorization server advertises `device_authorization_endpoint`, the system SHALL use the RFC 8628 device-code grant with a paste-back UI showing the verification URI and code. State and device codes SHALL be single-use and bound to the initiating server row.

#### Scenario: Browser authorization completes
- **WHEN** a user authorizes an OAuth-mode Notion server in the browser and the provider redirects back to the derived redirect URI
- **THEN** the callback validates the sealed state, exchanges the code with the PKCE verifier, and the server row becomes connected with the granted scopes recorded

#### Scenario: Headless device flow
- **WHEN** the instance has no public base URL and the server advertises a device authorization endpoint
- **THEN** the UI presents the verification URI and user code for paste-back, and the server connects once the device token poll succeeds

#### Scenario: Callback state mismatch rejected
- **WHEN** a callback arrives with an unknown, expired, or already-used state
- **THEN** it is rejected without exchanging any code

### Requirement: Token storage and refresh lifecycle
Tokens SHALL be stored encrypted at rest (AES-256-GCM envelope under the instance master key), keyed per server row, and never serialized to clients beyond presence. Before a dial, a token inside its refresh margin SHALL be renewed via the stored refresh token; the response's issuer (`iss`, RFC 9207) SHALL be validated against the selected authorization server when present. A failed refresh SHALL leave the stored token in place (fail-open dial, matching the connection-credential convention), persist the server row's `expired` status with the provider error, and exit `expired` only through reauthorization.

#### Scenario: Refresh within margin before a dial
- **WHEN** an agent run dials a server whose access token expires within the refresh margin and the provider accepts the refresh token
- **THEN** the dial uses the renewed token and the stored envelope and expiry are updated

#### Scenario: Failed refresh fails open and marks expired
- **WHEN** a refresh attempt is refused by the provider
- **THEN** the dial proceeds with the stored token, the server row shows `expired` with the provider's error, and a successful reauthorization later clears it

#### Scenario: Tokens never echoed
- **WHEN** any API view of the server row is read
- **THEN** access and refresh tokens are absent (presence/hint at most)

### Requirement: Launch presets
The add-server UI SHALL ship pre-filled templates for Notion (`https://mcp.notion.com/mcp`, streamable HTTP, OAuth) and Sentry (`https://mcp.sentry.dev/mcp/{organization}`, streamable HTTP, OAuth) that complete registration and authorization with no instance-admin configuration.

#### Scenario: Notion preset connects with zero registration
- **WHEN** a workspace admin adds the Notion preset and completes the browser authorization
- **THEN** the server lists tools and its status becomes connected
