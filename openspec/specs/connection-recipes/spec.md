# connection-recipes Specification

## Purpose

Defines the integration recipe catalog contract: how services declare their connection shape (transport, auth, endpoint, verbs, scopes, probe, webhooks), how a connect-time base-URL origin parameter is validated and resolved, and how per-service connection uniqueness interacts with multiple provider origins (SaaS and self-managed instances).

## Requirements

### Requirement: Recipe base-URL parameter
A recipe MAY declare a base-URL parameter consisting of a field name, a default origin (the SaaS host), and help text. When declared, the connect flow SHALL present a labeled origin field preset to the default, and SHALL accept only origin values: a scheme of `http` or `https`, a host, and an optional port — no path, query, fragment, userinfo, or trailing slash. When the parameter is not declared, the recipe's endpoint is fixed and the connect flow SHALL NOT present an origin field.

#### Scenario: Origin field presets the SaaS default
- **WHEN** a user opens the connect flow for a recipe that declares a base-URL parameter with default `https://gitlab.com`
- **THEN** the origin field is shown labeled and prefilled with `https://gitlab.com`, and connecting without editing resolves the connection to that origin

#### Scenario: Non-origin values rejected
- **WHEN** a connect request supplies `https://gitlab.example.com/api/v4` (path) or `gitlab.example.com` (no scheme) or `ftp://host` as the origin
- **THEN** the request is rejected with a validation error naming the base-URL field, and no connection or materialized server is created

#### Scenario: Undeclared recipes ignore origin
- **WHEN** a connect request supplies an origin override for a recipe that does not declare a base-URL parameter
- **THEN** the override is ignored and the connection resolves to the recipe's fixed endpoint

### Requirement: Endpoint derivation from origin
For a connection with a resolved origin, the system SHALL resolve the recipe's endpoint and verb paths against that origin: the materialized MCP server's URL (URL transports) and every declared HTTP verb's request URL are the recipe's fixed path appended to the resolved origin. The resolved origin SHALL be immutable after connect — changing origins requires disconnecting and reconnecting, never an in-place edit of the materialized server's URL or the connection's origin.

#### Scenario: Self-managed origin materializes a self-managed server
- **WHEN** the GitLab recipe is connected with origin `https://gitlab.example.com`
- **THEN** the materialized MCP server or declared-call endpoint resolves to `https://gitlab.example.com` with the recipe's fixed path prefix, and the stored token is attached to that origin's requests only

#### Scenario: Origin immutable after connect
- **WHEN** a client attempts to change a connected connection's origin through any update or reauthorize path
- **THEN** the attempt is refused and the connection keeps its original origin

### Requirement: Connection uniqueness per service and origin
Connection uniqueness SHALL be enforced per (service, resolved origin): a workspace that already holds a connection for a service on one origin SHALL still be able to connect the same service on a different origin, and SHALL be rejected when connecting the same service on the same origin twice.

#### Scenario: Same service on two origins
- **WHEN** a workspace holds a GitLab connection for `https://gitlab.com` and attempts to connect GitLab again with origin `https://gitlab.example.com`
- **THEN** the second connection is created as an independent connection with its own credential and materialized server

#### Scenario: Same service twice on one origin rejected
- **WHEN** a workspace holds a GitLab connection for `https://gitlab.example.com` and attempts to connect GitLab again with the same origin
- **THEN** the attempt fails with the existing per-service conflict error

### Requirement: GitLab REST recipe
The GitLab recipe SHALL be a PAT-authenticated HTTP-kind recipe whose base-URL parameter defaults to `https://gitlab.com`: it exposes a curated read-only verb surface and a read-write verb surface over the GitLab REST API path prefix, gates undeclared tools as write, probes via a declared read-only call, and preserves the recipe's existing webhook signing (secret token in the configured header). A personal access token with read-only scopes SHALL satisfy the read-only tier and be rejected from write verbs by the tier gating.

#### Scenario: Probe on a self-managed instance
- **WHEN** the GitLab recipe is connected with origin `https://gitlab.example.com` and a valid PAT for that instance
- **THEN** the declared probe call succeeds against that origin and the connection status becomes connected

#### Scenario: Unreachable self-managed origin
- **WHEN** the origin points at an instance that is unreachable or returns an authentication failure for the stored PAT
- **THEN** the probe persists an error status with the provider's error and the connection is recoverable by updating the token or disconnecting

### Requirement: GitHub recipe origin parameter
The GitHub recipe SHALL declare an optional base-URL parameter defaulting to `https://api.githubcopilot.com`, and its registration guidance SHALL state that GitHub Enterprise Server offers no remote MCP endpoint while GitHub Enterprise Cloud data-residency hosts follow the `copilot-api.<subdomain>.ghe.com` pattern.

#### Scenario: Enterprise Cloud data-residency origin
- **WHEN** the GitHub recipe is connected with origin `https://copilot-api.acme.ghe.com`
- **THEN** the materialized server dials the GitHub MCP endpoint path on that origin with the stored PAT
