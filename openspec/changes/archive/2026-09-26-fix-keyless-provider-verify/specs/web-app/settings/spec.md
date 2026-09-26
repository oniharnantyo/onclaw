# Spec Delta

## MODIFIED Requirements

### Requirement: Providers pane
The providers pane SHALL list the workspace's provider configs from the API — type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. Owners and Admins create and edit configs through a structured dialog (type select from the six catalog types, name, base URL shown only when the type requires or overrides it, password-style key input, and the catalog-mapping select for `-compatible` types); the key input is write-only — existing keys are never echoed back, replaced only. The dialog SHALL carry a "Verify connection" action that tests the currently entered form values against the provider before saving — the typed key for a new config, and the stored key when editing with a blank key field — reporting the outcome inline (busy, success, or the provider error); the outcome is never persisted and does not gate the save button. For a provider type that does not require an API key, the dialog SHALL render a "This endpoint needs no API key" checkbox above the key field (unchecked by default, and not rendered when the type requires a key or a key is stored): checking it SHALL disable the API key field and enable the Verify connection action without a key, and unchecking it SHALL re-enable the field; the checkbox is frontend state only and is never persisted. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. The pane SHALL show an inviting empty state when no providers are configured.

#### Scenario: Create an OpenAI config
- **WHEN** an Owner submits type "OpenAI", name "Acme prod", key through the create dialog
- **THEN** the row appears with type badge OpenAI, key set "…last4", enabled on, and a creation toast

#### Scenario: Compatible type form
- **WHEN** the user selects type "openai-compatible" in the create dialog
- **THEN** the base URL field becomes required and help text explains the origin rule (no version path)

#### Scenario: Key never echoed
- **WHEN** the user edits "Acme prod" without touching the key field
- **THEN** the form never displays the stored key, and PATCH preserves it

#### Scenario: Dialog verify on unsaved values
- **WHEN** the user types a key in the create dialog and clicks "Verify connection"
- **THEN** a busy state shows, then an inline result reports success or the provider error, and the dialog stays open with the entered values

#### Scenario: Dialog verify with stored key
- **WHEN** the user edits a config with a stored key, leaves the key field blank, and clicks "Verify connection"
- **THEN** the verification runs against the stored key together with the form's current type and base URL

#### Scenario: Keyless checkbox declares no-API-key
- **WHEN** the user selects type "openai-compatible" and ticks "This endpoint needs no API key"
- **THEN** the API key field becomes disabled, the Verify connection action becomes enabled, and unchecking the box re-enables the key field and returns the action to its disabled state

#### Scenario: Keyless checkbox verify runs without a key
- **WHEN** the checkbox is ticked, the base URL is entered, and the user clicks "Verify connection"
- **THEN** the verification runs keyless and reports the inline result; nothing about the checkbox state is submitted or persisted

#### Scenario: Verify does not gate save
- **WHEN** the user saves the dialog without clicking "Verify connection"
- **THEN** the save proceeds exactly as before — verification is optional and never disables save

#### Scenario: Verify transient result
- **WHEN** the user clicks verify on "Acme prod"
- **THEN** a busy state shows, then an inline banner reports success or the provider error; the row data is unchanged

#### Scenario: Empty state
- **WHEN** the workspace has no provider configs
- **THEN** the pane shows an empty state inviting configuration, visible to Members too (read is a permission)
