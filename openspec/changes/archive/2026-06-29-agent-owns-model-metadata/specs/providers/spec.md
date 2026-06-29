## MODIFIED Requirements

### Requirement: Provider profiles are connection-only and persisted in a sqlite store

The system SHALL persist provider profiles (`name`, `provider_type`, `api_base`,
`enabled`) in a sqlite database, queryable and listable, keyed by a unique profile name.
A provider profile SHALL NOT store a model; it represents a connection only. Profile
fields other than the API key SHALL be stored as plaintext.

#### Scenario: Add and retrieve a connection-only provider profile

- **WHEN** a profile `claude` is added with provider_type `anthropic` and a base_url
- **THEN** `provider list` includes `claude`, and `provider` lookup by name returns the name, provider_type, and api_base, and no model is stored or prompted for

#### Scenario: Keyless provider has no secret

- **WHEN** a profile `local` of provider_type `ollama` is added with no API key
- **THEN** the profile is stored and usable, and no `config_secrets` row exists for `local`

#### Scenario: Profile names are unique

- **WHEN** a profile is added with a name that already exists
- **THEN** the system rejects the duplicate and leaves the existing profile unchanged