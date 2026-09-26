## ADDED Requirements

### Requirement: OAuth app registration

Instance admins SHALL be able to register one OAuth app per provider: client id and client secret, stored encrypted with the instance-scoped key derivation and never returned in full after save (the secret is write-only; presence and last-4 hint only). The system SHALL display the exact redirect URI to configure at the provider, derived from the instance's public base URL, and SHALL report per-provider registration status that the connections gallery consumes to flip OAuth recipe cards to available.

#### Scenario: Register the Atlassian app

- **WHEN** an instance admin saves a client id and client secret for provider `atlassian`
- **THEN** the credentials are stored encrypted, the redirect URI is displayed for configuration at the provider, and Atlassian OAuth recipes report as available to workspaces

#### Scenario: Secret is write-only

- **WHEN** any client reads the OAuth app registration afterwards
- **THEN** the client secret is absent, with a last-4 hint only
