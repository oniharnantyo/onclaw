# Spec Delta

## ADDED Requirements

### Requirement: Document preset rows

The database SHALL store document presets as rows carrying a store-assigned id, a user-visible name unique per format, the format the preset applies to (only `pdf` in v1), and the preset's presentation configuration as opaque JSON. Presets SHALL survive restarts, and every change MUST be applied atomically. Deleting a preset MUST remove its row only and MUST NOT touch any generated file.

#### Scenario: Preset survives restart

- **WHEN** a preset is saved and the app restarts
- **THEN** the row still exists with the same configuration

#### Scenario: Preset names are unique per format

- **WHEN** a preset is created with a name another `pdf` preset already uses
- **THEN** the store rejects it as a duplicate

#### Scenario: Delete removes only the row

- **WHEN** a preset is deleted
- **THEN** its row is gone and generated documents in agent workspaces are untouched
