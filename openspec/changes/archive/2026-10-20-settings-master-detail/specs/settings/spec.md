# Spec Delta

## MODIFIED Requirements

### Requirement: Providers pane

The settings menu SHALL let the user add, edit, and remove model providers
from a catalog of LLM provider types: `openai`, `anthropic`, `gemini`,
`openrouter`, `openai-compatible`, and `anthropic-compatible` (decision/routing
provider types are out of scope). Each provider entry SHALL hold its API
credential and SHALL expose its stored model list (see "Provider model list")
for selection by agents. Credentials MUST be persisted with the rest of
configuration.

The Providers pane SHALL present a master-detail layout: a provider list
column and, for the selected provider, a detail view. Creating a provider
SHALL present the same detail layout with the type select enabled. The detail
form SHALL be driven by the selected type's catalog entry: display label, Base
URL placeholder, and whether Base URL is required (`openai-compatible` and
`anthropic-compatible` require it; the canonical types accept an optional
override). The provider type (the provider's API format) MUST be disabled
while editing an existing provider. Each provider list entry SHALL offer the
provider's enable state through the detail view's toggle; a disabled provider
MUST NOT serve agent requests. Selecting a provider MUST NOT require a modal
dialog.

#### Scenario: Add a provider

- **WHEN** the user adds a provider with a valid API credential
- **THEN** the provider appears in the list column, is selected, and its
  stored models become selectable in agent configuration

#### Scenario: Add a compatible-endpoint provider

- **WHEN** the user selects `openai-compatible` or `anthropic-compatible` and
  submits without a Base URL
- **THEN** the form rejects the submission, indicating Base URL is required
  for that type

#### Scenario: Remove a provider

- **WHEN** the user removes a provider that no agent currently uses
- **THEN** the provider disappears from settings and from agent model options

#### Scenario: Remove a provider still in use

- **WHEN** the user removes a provider that one or more agents reference
- **THEN** a confirmation warns which agents still use it before the removal
  proceeds

#### Scenario: Disable a provider

- **WHEN** the user toggles a provider off
- **THEN** the provider is shown as disabled (its status dot and detail view)
  and no longer serves agent requests until re-enabled

#### Scenario: Edit a provider's type

- **WHEN** the user edits an existing provider
- **THEN** the provider type is shown but cannot be changed

#### Scenario: Selecting a provider shows its detail

- **WHEN** the user selects a provider in the list column
- **THEN** the detail view shows that provider's name header with its toggle,
  Base URL, type, key field, and model list, without any modal dialog

### Requirement: Provider connection verification

The provider service SHALL verify a provider's credentials by making a minimal
authenticated request to the provider endpoint. Verification SHALL be
available in two forms: against unsaved form values (draft: type, base URL,
key, falling back to the stored key when editing and the key field is blank)
and against a saved provider. The outcome (success, or failure with the
endpoint's error) SHALL be displayed without saving anything; a failed
verification MUST NOT block saving.

#### Scenario: Verify draft credentials

- **WHEN** the user triggers draft verification in the create form with a key
  typed in
- **THEN** the service tests exactly the entered values against the provider
  and reports success or the failure reason, and nothing is persisted

#### Scenario: Verify draft against stored key

- **WHEN** the user edits a provider with a stored key, leaves the key field
  blank, and verifies
- **THEN** the verification runs against the stored key combined with the
  form's type and base URL

#### Scenario: Verify a saved provider

- **WHEN** the user triggers verification from a provider's detail view and
  that provider has a key set
- **THEN** the outcome (success or failure with reason) is shown in the
  detail view without modifying the provider

### Requirement: Provider row identity

Each provider list entry SHALL lead with a brand logo tile sized consistently
with the entry (known providers use their brand mark; unknown provider types
fall back to an initial glyph on the same tile), followed by the provider
name and a status dot reflecting enabled and key state. The detail view SHALL
reuse the brand tile beside the provider name with its type label, and SHALL
show the key-state hint in the mono face (per credential secrecy). Logo tiles
SHALL render legibly in both themes.

#### Scenario: Provider row shows its brand

- **WHEN** the Providers pane lists a provider with a known brand mark
- **THEN** the list entry leads with that provider's logo tile before its name

#### Scenario: Unknown provider falls back

- **WHEN** the Providers pane lists a provider type without a known brand mark
- **THEN** the entry shows an initial-glyph tile in the same size and position

#### Scenario: Detail view carries the identity

- **WHEN** the user selects a provider
- **THEN** the detail view header shows the brand tile, the provider name,
  its type label, and the key-state hint

### Requirement: Provider credential secrecy

The provider service MUST NOT return a provider's API key to any frontend.
Provider reads and listings SHALL instead expose whether a key is set
(`KeySet`) and at most a short non-secret hint derived from it (the last 4
characters). Updating a provider with a blank key SHALL keep the stored key;
only a non-blank key input replaces it.

#### Scenario: Key never round-trips

- **WHEN** the frontend lists or reads providers
- **THEN** no response contains the raw API key, only `KeySet` and the hint

#### Scenario: Edit without retyping the key

- **WHEN** the user edits a provider that has a stored key and submits the
  form with the key field left blank
- **THEN** the stored key is preserved

#### Scenario: Replace a stored key

- **WHEN** the user edits a provider and submits a non-blank key
- **THEN** the stored key is replaced and the hint reflects the new key

#### Scenario: Reveal unmasks typed input only

- **WHEN** the user toggles the key field's reveal control
- **THEN** only the input typed in the current session is unmasked; the
  stored key is never returned to or displayed by the frontend

## ADDED Requirements

### Requirement: Provider model list

Each provider SHALL have a stored model list — the authoritative set of
models available from it. The store SHALL persist the list per provider (see
the data-store capability); the Providers pane's detail view SHALL present it
under a "Model list" section.

The list SHALL be seeded for new and existing providers: canonical types
(`openai`, `anthropic`, `gemini`) from the built-in model catalog;
`openai-compatible`, `anthropic-compatible`, and `openrouter` start empty
(their models are not known until added or refreshed). A refresh affordance
SHALL re-pull a live listing for the live-listed types (or re-seed from the
built-in catalog for canonical types) and upsert it into the stored list; a
failed refresh MUST leave the stored list untouched, and a successful one
MUST preserve user-created rows and enable states.

The user SHALL add a model to the list with a model id, a context window
(tokens), and image-input ("Vision") capability; SHALL edit and remove each
row; and SHALL toggle each row's enabled state. Each row SHALL display its
model id in the mono face with badges for its context window (human-readable)
and Vision capability when set.

Only enabled models SHALL be offered in agent configuration and the
composer's model pickers. A model override or turn referencing a disabled or
removed model MUST fail with a clear error rather than silently substituting.

#### Scenario: Canonical provider's list is seeded

- **WHEN** the user creates (or already has) a provider of a canonical type
- **THEN** its model list contains the built-in catalog's models for that
  type with their capability data, enabled

#### Scenario: Compatible provider starts empty

- **WHEN** the user creates a provider of a live-listed type
- **THEN** its model list starts empty and the detail view offers adding a
  model and refreshing the live listing

#### Scenario: Add a model

- **WHEN** the user adds a model with an id, a context window, and Vision
  enabled
- **THEN** the row appears with the id in mono, the human-readable context
  badge, and the Vision badge, and (enabled) becomes selectable in agent
  configuration

#### Scenario: Refresh upserts without discarding

- **WHEN** the user triggers refresh on a live-listed provider whose endpoint
  returns its listing
- **THEN** listed model ids are upserted into the stored list while
  user-created rows and existing enable states are preserved

#### Scenario: Failed refresh changes nothing

- **WHEN** a refresh cannot reach the provider endpoint
- **THEN** the stored list is unchanged and the failure is reported

#### Scenario: Disable a model

- **WHEN** the user toggles a model off
- **THEN** it disappears from agent configuration and composer model pickers,
  and a turn or override referencing it fails with a clear error

#### Scenario: Delete a model

- **WHEN** the user deletes a stored model
- **THEN** the row disappears from the list and pickers, and a turn
  referencing it fails with a clear error
