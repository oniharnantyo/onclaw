# Spec Delta

## ADDED Requirements

### Requirement: Provider model rows

The store SHALL persist each provider's model list as model rows: a model id
unique per provider, the model's context window in tokens, its image-input
and PDF-input capability flags, and an enabled flag. Row identity SHALL be
store-assigned and immutable; enabling, capability fields, and the model id's
row are user-editable configuration. Deleting a provider MUST remove its
model rows. Forward-only migrations SHALL create the table and seed existing
canonical-type providers' rows from the built-in catalog.

#### Scenario: Model ids are unique per provider

- **WHEN** a model row is created with a model id the provider already has
- **THEN** the store rejects it as a duplicate

#### Scenario: Deleting a provider removes its models

- **WHEN** a provider is deleted
- **THEN** none of its model rows remain

#### Scenario: Migration seeds canonical providers

- **WHEN** the migration runs on a database with existing canonical-type
  providers
- **THEN** each of those providers has the built-in catalog's models as
  enabled rows with their capability data
