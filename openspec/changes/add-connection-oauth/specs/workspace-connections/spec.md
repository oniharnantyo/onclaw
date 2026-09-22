## ADDED Requirements

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
