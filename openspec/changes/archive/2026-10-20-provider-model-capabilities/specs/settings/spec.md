# Spec Delta

## MODIFIED Requirements

### Requirement: Provider model list

Each provider SHALL have a stored model list — the authoritative set of
models available from it. The store SHALL persist the list per provider (see
the data-store capability); the Providers pane's detail view SHALL present it
under a "Model list" section.

The list SHALL be seeded for new and existing providers: canonical types
(`openai`, `anthropic`, `gemini`) from the built-in model catalog;
`openai-compatible`, `anthropic-compatible`, and `openrouter` start empty
(their models are not known until the user adds them). The list is
hand-curated: the pane SHALL NOT offer a refresh of the list from the
provider endpoint.

The user SHALL add a model to the list with a model id, a context window
(tokens), input checkboxes for image, PDF, and video, and an ordered
reasoning-effort list of free-form values; SHALL edit and remove each row;
and SHALL toggle each row's enabled state. Each row SHALL display its model
id in the mono face with badges for its context window (human-readable) and
its image, PDF, and video input capabilities when set.

A row's declared reasoning-effort set SHALL be authoritative for that row:
the composer's effort picker SHALL offer exactly the declared values, in
declared order. A row with an empty effort declaration SHALL resolve to
`high` alone. Only enabled models SHALL be offered in agent configuration
and the composer's model pickers. A model override or turn referencing a
disabled or removed model MUST fail with a clear error rather than silently
substituting.

#### Scenario: Canonical provider's list is seeded

- **WHEN** the user creates (or already has) a provider of a canonical type
- **THEN** its model list contains the built-in catalog's models for that
  type with their capability data, enabled

#### Scenario: Compatible provider starts empty

- **WHEN** the user creates a provider of a live-listed type
- **THEN** its model list starts empty and the detail view offers adding a
  model, with no refresh affordance

#### Scenario: Add a model

- **WHEN** the user adds a model with an id, a context window, the image and
  video checkboxes on, and effort rows `none` and `high`
- **THEN** the row appears with the id in mono, the human-readable context
  badge, the image and video badges, and (enabled) becomes selectable in
  agent configuration with `none` and `high` as its effort options in that
  order

#### Scenario: Declared efforts drive the effort picker

- **WHEN** a model row declares a reasoning-effort set
- **THEN** the composer's effort picker for that model offers exactly the
  declared values in declared order

#### Scenario: Undeclared efforts default to high

- **WHEN** a model row has no declared effort values
- **THEN** the composer's effort picker for that model offers `high` alone

#### Scenario: Disable a model

- **WHEN** the user toggles a model off
- **THEN** it disappears from agent configuration and composer model pickers,
  and a turn or override referencing it fails with a clear error

#### Scenario: Delete a model

- **WHEN** the user deletes a stored model
- **THEN** the row disappears from the list and pickers, and a turn
  referencing it fails with a clear error
