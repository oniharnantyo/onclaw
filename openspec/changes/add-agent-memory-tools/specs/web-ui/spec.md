## ADDED Requirements

### Requirement: Global embedding configuration page

The system SHALL provide an Embeddings configuration page under the Configuration menu where a
user sets the global embedding default used by memory. The page SHALL expose a provider selector
(populated from the existing provider profiles), an embedding model override field, and an
optional API base field. The page SHALL load its current values from
`GET /api/config/embeddings` and persist them via `PUT /api/config/embeddings`. The configuration
SHALL be the global default that every agent inherits unless it defines its own per-agent
embedding override.

#### Scenario: A user sets the global embedding default

- **WHEN** the user selects a provider profile and model on the Embeddings page and saves
- **THEN** the values are persisted to the configuration store and agents without a per-agent
  override use them for embeddings

#### Scenario: The page reflects the stored configuration

- **WHEN** the user opens the Embeddings page
- **THEN** the provider, model, and API base fields are populated from the currently stored
  global embedding configuration
