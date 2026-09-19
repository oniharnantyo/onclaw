## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the thirteen sections — Workspace, Providers, Members & roles, Memory, Gateways, Integrations, MCP servers, Skills, Tools, Hooks, API keys, Notifications, and Storage — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. Settings SHALL render full-screen: the icon rail is not rendered on `/settings` routes, and a slim header above the section navigation carries a `← Back` control that returns to the route the user came from (the most recent non-settings route in the current tab, falling back to `/c` when there is none). `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the section nav
- **THEN** the keys section renders with its manage controls and the URL reads /settings/keys

#### Scenario: Gateways reachable
- **WHEN** the user selects "Gateways" in the section nav
- **THEN** the gateways section renders and the URL reads /settings/gateways

#### Scenario: Tools reachable
- **WHEN** the user selects "Tools" in the section nav
- **THEN** the tools section renders and the URL reads /settings/tools

#### Scenario: Deep link
- **WHEN** a member opens /settings/tools directly
- **THEN** the tools section renders without passing through any other section

#### Scenario: Unknown section
- **WHEN** a member opens /settings/nonexistent
- **THEN** the app redirects to /settings/workspace

#### Scenario: Sidebar hidden on settings
- **WHEN** the user is on any /settings route at 1024px width
- **THEN** neither the workspace sidebar nor the icon rail is rendered; the header, section nav column, and section content fill the viewport

#### Scenario: Back returns to origin
- **WHEN** the user opens Settings from the chats view, navigates between sections, and clicks "← Back"
- **THEN** the app returns to the chats view; a deep-linked settings tab with no in-app history falls back to /c

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name, timezone, and the workspace default model through the API on explicit save (thread retention remains a local workspace field until its domain integrates; the workspace URL is display-only and shows no plan indicator). The timezone field SHALL be a searchable selector over the full IANA zone list, each entry showing its UTC offset. The default model SHALL be picked provider-first: a provider select over the workspace's configured provider configs, then a model combobox fed by that provider's model-catalog endpoint (free-text when the catalog resolves nothing); the pair persists as the workspace default model, and clearing both removes the default. When the server refuses a change (for example clearing the default while inherit-agents exist) the pane SHALL surface the refusal as an error toast naming the reason.

#### Scenario: Save settings
- **WHEN** a member with workspace.write saves a new workspace name or timezone
- **THEN** the change persists (reload keeps it) and a toast confirms

#### Scenario: Save default model
- **WHEN** a member with workspace.write picks provider "Acme prod" and model "gpt-4o" and saves
- **THEN** the pair persists as the workspace default model (reload keeps it)

#### Scenario: Model list follows provider
- **WHEN** the user selects a different provider in the default-model picker
- **THEN** the model combobox re-queries that provider's catalog and the previously chosen model id is cleared

#### Scenario: Clearing the default is refused while inherited
- **WHEN** a member clears the default model while agents inherit it and saves
- **THEN** the API refuses the change and the pane shows an error toast naming the inherit-agents count

#### Scenario: Save without permission
- **WHEN** a member without workspace.write attempts to save
- **THEN** the save action is unavailable (or rejected with a forbidden toast)

#### Scenario: Searchable timezone
- **WHEN** the user types "jakar" into the timezone field
- **THEN** "Asia/Jakarta" with its UTC offset is selectable

### Requirement: Providers pane
The providers pane SHALL list the workspace's provider configs from the API — type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. Owners and Admins create and edit configs through a structured dialog (type select from the six catalog types, name, base URL shown only when the type requires or overrides it, password-style key input, and the catalog-mapping select for `-compatible` types); the key input is write-only — existing keys are never echoed back, replaced only. The dialog SHALL carry a "Verify connection" action that tests the currently entered form values against the provider before saving — the typed key for a new config, and the stored key when editing with a blank key field — reporting the outcome inline (busy, success, or the provider error); the outcome is never persisted and does not gate the save button. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. The pane SHALL show an inviting empty state when no providers are configured.

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

#### Scenario: Verify does not gate save
- **WHEN** the user saves the dialog without clicking "Verify connection"
- **THEN** the save proceeds exactly as before — verification is optional and never disables save

#### Scenario: Verify transient result
- **WHEN** the user clicks verify on "Acme prod"
- **THEN** a busy state shows, then an inline banner reports success or the provider error; the row data is unchanged

#### Scenario: Empty state
- **WHEN** the workspace has no provider configs
- **THEN** the pane shows an empty state inviting configuration, visible to Members too (read is a permission)

## ADDED Requirements

### Requirement: Memory pane
The Memory section SHALL present the workspace memory pane as three tabs — **Configuration**, **Facts**, and **Morning report** — with **Configuration** as the tab in view when the pane opens. The Facts tab SHALL remain the derived-facts browser and the Morning report tab the consolidation report, as built. The Configuration tab SHALL expose the visibility default posture (narrow / org-shared), the ingestion master toggle, the memory side-call model (agent default, or a specific provider+model pair), and the embedding configuration: an embedding provider select over the workspace's configured provider configs, an embedding model field (dropdown over the provider's embedding-classified catalog models when any resolve, free text otherwise), and a **Dimension** dropdown offering "Auto — detect on test" plus OnClaw's supported embedding dimensions — 768, 1024, 1536, 2048, and 3072. Selecting an embedding model whose dimension OnClaw knows SHALL preselect that dimension; "Auto" leaves the dimension unset until the connection test discovers it. Test connection SHALL remain the authority: its discovered dimension fills an Auto selection, and a stored or chosen dimension that mismatches the endpoint's returned dimension SHALL be reported as an error, not silently stored.

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
