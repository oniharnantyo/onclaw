# Spec Delta

## MODIFIED Requirements

### Requirement: Providers pane
The providers pane SHALL present the workspace's provider configs from the API under two internal tabs — **Language models** (default) and **Decision** — listing rows with type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. `typesafe` configs SHALL list only under the Decision tab, which SHALL also carry a hint that decision providers power routing calls and never serve chat or agent models; language-model types SHALL list only under Language models. Owners and Admins create and edit configs through a structured dialog (type select whose options are grouped into "Language models" and "Decision", name, base URL shown only when the type requires or overrides it, password-style key input, and the catalog-mapping select for `-compatible` types); the key input is write-only — existing keys are never echoed back, replaced only. The dialog SHALL carry a "Verify connection" action that tests the currently entered form values against the provider before saving — the typed key for a new config, and the stored key when editing with a blank key field — reporting the outcome inline (busy, success, or the provider error); the outcome is never persisted and does not gate the save button. For a provider type that does not require an API key, the dialog SHALL render a "This endpoint needs no API key" checkbox above the key field (unchecked by default, and not rendered when the type requires a key or a key is stored): checking it SHALL disable the API key field and enable the Verify connection action without a key, and unchecking it SHALL re-enable the field; the checkbox is frontend state only and is never persisted. Selecting the `typesafe` type SHALL show the base URL field with the canonical systemone origin as its placeholder and SHALL NOT render the keyless checkbox. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. Each tab SHALL show an inviting empty state when it has no configs. Model-selection surfaces across the app — the agent config modal, the workspace default-model picker, and the memory pane's provider selects — SHALL list only language-model providers; decision providers SHALL be excluded from every model picker.

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

#### Scenario: Typesafe config lists under Decision only
- **WHEN** the workspace holds a `typesafe` config and an `openai` config
- **THEN** the Language models tab lists the `openai` config and the Decision tab lists the `typesafe` config, each tab hiding the other's rows

#### Scenario: Decision tab empty state
- **WHEN** the workspace holds no `typesafe` config and the user opens the Decision tab
- **THEN** the tab shows an inviting empty state naming decision providers as the memory routing backend

#### Scenario: Typesafe form fields
- **WHEN** the user selects type "typesafe" in the create dialog
- **THEN** the type select shows it under a "Decision" group, the base URL field shows the canonical systemone origin as placeholder, no keyless checkbox renders, and the API key field is required

#### Scenario: Model pickers exclude decision providers
- **WHEN** any model picker renders (agent config modal, workspace default model, memory side-call or embedding selects)
- **THEN** the provider options exclude every `typesafe` config

### Requirement: Memory pane
The Memory section SHALL present the workspace memory pane as three tabs — **Configuration**, **Facts**, and **Morning report** — with **Configuration** as the tab in view when the pane opens. The Facts tab SHALL remain the derived-facts browser and the Morning report tab the consolidation report, as built. The Configuration tab SHALL expose the visibility default posture (narrow / org-shared), the ingestion master toggle, the memory side-call model (agent default, or a specific provider+model pair), and the embedding configuration: an embedding provider select over the workspace's configured provider configs, an embedding model field (dropdown over the provider's embedding-classified catalog models when any resolve, free text otherwise), and a **Dimension** dropdown offering "Auto — detect on test" plus OnClaw's supported embedding dimensions — 768, 1024, 1536, 2048, and 3072. Selecting an embedding model whose dimension OnClaw knows SHALL preselect that dimension; "Auto" leaves the dimension unset until the connection test discovers it. Test connection SHALL remain the authority: its discovered dimension fills an Auto selection, and a stored or chosen dimension that mismatches the endpoint's returned dimension SHALL be reported as an error, not silently stored. The Configuration tab SHALL additionally expose a **Decision backend** block: an unchecked-by-default checkbox ("Use decision backend") whose checked state reveals a provider select (over `typesafe` decision providers only) and a model field prefilled with `jev-latest`; unchecked, no decision fields render and the pane stores nothing for them. Saving with the box checked but no provider selected SHALL be blocked with inline validation, mirroring the side-call "Specific model" flow. The block's help copy SHALL state that the decision backend classifies each turn's memory need in place of the side-call model and that entity routing (associative) requires the language model.

#### Scenario: Opens on Configuration
- **WHEN** a member opens Settings → Memory
- **THEN** the Configuration tab is in view without any interaction

#### Scenario: Dimension dropdown options
- **WHEN** the user opens the Dimension dropdown
- **THEN** the options are "Auto — detect on test", 768, 1024, 1536, 2048, and 3072

#### Scenario: Known model preselects its dimension
- **WHEN** the user selects an embedding model whose output dimension OnClaw knows (e.g. text-embedding-3-small)
- **THEN** the Dimension dropdown preselects that dimension

#### Scenario: Test discovers an Auto dimension
- **WHEN** the user leaves Dimension on Auto and runs Test connection successfully
- **THEN** the discovered dimension fills the dropdown and is stored with the settings on save

#### Scenario: Dimension mismatch is refused
- **WHEN** the chosen or stored dimension differs from the dimension the endpoint returns
- **THEN** the test reports the mismatch as an error and nothing is stored

#### Scenario: Decision backend defaults off
- **WHEN** a workspace with no stored decision configuration opens the Configuration tab
- **THEN** the Decision backend checkbox is unchecked and no provider or model fields render

#### Scenario: Checking reveals decision fields
- **WHEN** the user ticks the Decision backend checkbox
- **THEN** a provider select listing only `typesafe` decision providers and a model field prefilled with `jev-latest` appear

#### Scenario: Checked without provider blocks save
- **WHEN** the user saves with the checkbox ticked and no provider selected
- **THEN** the save is blocked with inline validation naming the missing provider, mirroring the side-call specific-model flow

#### Scenario: Unchecking clears the decision pair
- **WHEN** a workspace with a stored decision configuration unticks the checkbox and saves
- **THEN** the stored `decision_provider_id` and `decision_model` are removed and the intent gate returns to the LLM side-call
