# Spec Delta

## MODIFIED Requirements

### Requirement: Providers pane

The settings menu SHALL let the user add, edit, and remove model providers from a catalog of LLM provider types: `openai`, `anthropic`, `gemini`, `openrouter`, `openai-compatible`, and `anthropic-compatible` (decision/routing provider types are out of scope). Each provider entry SHALL hold its API credential and SHALL expose the models available from it for selection by agents. Credentials MUST be persisted with the rest of configuration.

The add/edit form SHALL be driven by the selected type's catalog entry: display label, Base URL placeholder, and whether Base URL is required (`openai-compatible` and `anthropic-compatible` require it; the canonical types accept an optional override). The type select MUST be disabled while editing an existing provider. Each provider row SHALL offer an inline enable/disable toggle; a disabled provider MUST NOT serve agent requests.

#### Scenario: Add a provider

- **WHEN** the user adds a provider with a valid API credential
- **THEN** the provider appears in the list and its models become selectable in agent configuration

#### Scenario: Add a compatible-endpoint provider

- **WHEN** the user selects `openai-compatible` or `anthropic-compatible` and submits without a Base URL
- **THEN** the form rejects the submission, indicating Base URL is required for that type

#### Scenario: Remove a provider

- **WHEN** the user removes a provider that no agent currently uses
- **THEN** the provider disappears from settings and from agent model options

#### Scenario: Remove a provider still in use

- **WHEN** the user removes a provider that one or more agents reference
- **THEN** a confirmation warns which agents still use it before the removal proceeds

#### Scenario: Disable a provider

- **WHEN** the user toggles a provider off
- **THEN** the provider is shown as disabled and no longer serves agent requests until re-enabled

#### Scenario: Edit a provider's type

- **WHEN** the user edits an existing provider
- **THEN** the provider type is shown but cannot be changed

## ADDED Requirements

### Requirement: Provider credential secrecy

The provider service MUST NOT return a provider's API key to any frontend. Provider reads and listings SHALL instead expose whether a key is set (`KeySet`) and at most a short non-secret hint derived from it (the last 4 characters). Updating a provider with a blank key SHALL keep the stored key; only a non-blank key input replaces it.

#### Scenario: Key never round-trips

- **WHEN** the frontend lists or reads providers
- **THEN** no response contains the raw API key, only `KeySet` and the hint

#### Scenario: Edit without retyping the key

- **WHEN** the user edits a provider that has a stored key and submits the form with the key field left blank
- **THEN** the stored key is preserved

#### Scenario: Replace a stored key

- **WHEN** the user edits a provider and submits a non-blank key
- **THEN** the stored key is replaced and the hint reflects the new key

### Requirement: Provider connection verification

The provider service SHALL verify a provider's credentials by making a minimal authenticated request to the provider endpoint. Verification SHALL be available in two forms: against unsaved form values (draft: type, base URL, key, falling back to the stored key when editing and the key field is blank) and against a saved provider. The outcome (success, or failure with the endpoint's error) SHALL be displayed without saving anything; a failed verification MUST NOT block saving.

#### Scenario: Verify draft credentials

- **WHEN** the user clicks "Verify connection" in the add/edit dialog with a key typed in
- **THEN** the service tests exactly the entered values against the provider and reports success or the failure reason, and nothing is persisted

#### Scenario: Verify draft against stored key

- **WHEN** the user edits a provider with a stored key, leaves the key field blank, and verifies
- **THEN** the verification runs against the stored key combined with the form's type and base URL

#### Scenario: Verify a saved provider

- **WHEN** the user triggers verification on a provider row that has a key set
- **THEN** the outcome (success or failure with reason) is shown on that row without modifying the provider

### Requirement: Settings navigation layout

The settings menu SHALL present a two-column layout: a left settings-nav sidebar and a right content pane. The sidebar SHALL list the settings panes grouped into sections — Providers under MODELS; Skills, Tools, Hooks, and MCP under AUTOMATION — each with an icon and its label, and SHALL offer a search input that filters the pane list by label. The content pane SHALL show the active pane's title, a one-line description, and the pane body. Selecting a sidebar entry switches the content pane.

#### Scenario: Open settings

- **WHEN** the user opens the settings menu
- **THEN** the sidebar shows the grouped pane list and the content pane shows the first pane (Providers)

#### Scenario: Switch panes

- **WHEN** the user selects a different pane in the sidebar
- **THEN** the content pane swaps to that pane with its title and description

#### Scenario: Search filters panes

- **WHEN** the user types in the sidebar search input
- **THEN** the pane list filters to entries matching the text, and clearing it restores the full list

#### Scenario: Theme parity

- **WHEN** the user toggles between light and dark themes while in settings
- **THEN** the sidebar, content pane, and provider surfaces render legibly in the selected theme
