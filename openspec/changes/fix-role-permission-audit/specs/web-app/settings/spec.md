## MODIFIED Requirements

### Requirement: Members pane
The members pane SHALL render members from the members API — email, name, avatar (avatar_url image with initials fallback), role, joined date. Owners and Admins add members through an invite dialog — email plus a role selected from the workspace's real roles (fetched once per workspace) — change roles inline, and remove members through the API; guard rejections (peers unmanageable, last_owner_protected, missing permission) surface as toasts. The pane SHALL gate its mutation affordances by permission: the invite control, inline role selectors, and remove actions render only for holders of `members.write` (and `members.remove` for removal), and are absent for Members, who see the roster read-only. Password-less members (added by email without a password) show an "Invited" hint; the member's own row is labeled "You".

#### Scenario: Invite validation
- **WHEN** the invite dialog's email field contains "not-an-email"
- **THEN** the Invite button is disabled

#### Scenario: Guard rejection toast
- **WHEN** an admin attempts to manage a peer admin and the server rejects
- **THEN** a toast explains admins cannot manage each other; nothing changes

#### Scenario: Invited member hint
- **WHEN** a member was added without a password
- **THEN** the row shows an "Invited" hint instead of a login-capable status

#### Scenario: Invite via dialog
- **WHEN** an Owner submits the invite dialog with a valid email and role
- **THEN** the dialog closes, the member list refreshes, and a confirmation toast appears

#### Scenario: Member sees a read-only roster
- **WHEN** a Member opens the members pane
- **THEN** the roster renders fully but no invite control, role selector, or remove action is shown

### Requirement: Tools pane (API-backed)
The Tools pane SHALL render non-toggleable (always-on) catalog tools in a "Channel Tool" group section at the top of the pane — a section header, then one row per non-toggleable tool with its display name, one-line description, and an always-on badge where other rows carry the enable toggle ("Always on · channel runs" for Channel Post and Channel History, "Always on · facilitator only" for Close Work Session); these rows SHALL render no toggle and no gear (none is configurable), and the section SHALL reflect the endpoint payload — which reports non-toggleable tools as enabled regardless of stored rows. Every toggleable tool SHALL list as a flat list below the section — one row per tool with its display name, one-line description, and an enable toggle reflecting (and driving) the workspace-wide tool status; toggling requires the `tools.write` permission — toggles and gear buttons render only for `tools.write` holders, and Members see every row read-only (no toggle, no gear) — and toggle attempts SHALL surface guard rejections as toasts. Configurable tools SHALL additionally render a gear button opening a structured config dialog rendered from the tool's config-field schema: one labeled control per field — `secret` fields as password-style write-only inputs (existing values shown only as hints, replaced on save), `text` as inputs, `number` as numeric inputs, `boolean` as toggles, `enum` as selects fed by the schema's options — with help text and a Save action. For `web.search` the dialog SHALL additionally render a provider-stack list editor: one row per configured entry with a reorder control pair (disabled at the list bounds), a name input, a provider select fed by the registry options, the credential field that provider requires (a password-style write-only key input showing the stored last-4 hint, or a base-URL input for SearXNG), and a remove action; an Add control appends a new row, and a timeout field bounds the per-attempt request timeout. Rows in the first three positions SHALL be labeled "in rotation"; rows below SHALL render dimmed and labeled "standby", and reordering SHALL re-evaluate the labels immediately. Removing a row SHALL not persist until Save; saving SHALL submit the whole ordered list with entry ids, and a row whose credential field is left empty SHALL keep its stored credential (reorder-safe via the stable entry id). Validation failures SHALL render inline per row (empty name, missing credential for a key-requiring provider, duplicate names) and block Save. The pane SHALL NOT offer raw JSON editing for any structured value.

#### Scenario: Flat list with toggles
- **WHEN** a Member opens the Tools pane
- **THEN** every toggleable catalog tool appears as a flat row — display name, description, toggle — with no group headings, driven by the workspace tools endpoint

#### Scenario: Channel Tool section with always-on badges
- **WHEN** a Member opens the Tools pane
- **THEN** Channel Post, Channel History, and Close Work Session render under a "Channel Tool" section header above the flat list, each with its display name, description, and an always-on badge in place of a toggle, and no toggle or gear on the row

#### Scenario: Badge rows ignore stale disabled state
- **WHEN** the workspace holds a stored disabled settings row for Channel History written before the tool became non-toggleable
- **THEN** the payload reports it enabled and the row renders the same always-on badge as the other two

#### Scenario: Gear opens config dialog
- **WHEN** the user activates the gear on "Web Search"
- **THEN** the dialog opens as a provider-stack list editor — per-row name, provider select, credential, reorder, remove, an Add control, and the per-attempt timeout field

#### Scenario: Rotation labels track order
- **WHEN** the user moves a standby row into the top three (or a rotation row out)
- **THEN** the in-rotation/standby labels and dimming re-evaluate immediately, before Save

#### Scenario: Reorder keeps credentials
- **WHEN** the user swaps two configured rows and saves without retyping either key
- **THEN** each row keeps its own stored credential, matched by entry id rather than position

#### Scenario: Browser config dialog
- **WHEN** the user opens the Browser config dialog
- **THEN** it exposes headless toggle, remote CDP URL, max pages, idle timeout, and action timeout, one labeled control per property, with help text explaining that a set CDP URL makes headless irrelevant

#### Scenario: Disabled until configured
- **WHEN** no search entry is configured for the workspace
- **THEN** the Web Search row shows its toggle as unavailable with an explanatory hint, and saving one valid entry enables the toggle

#### Scenario: Secret never echoed
- **WHEN** the user reopens the Web Search dialog after saving an API key
- **THEN** each row's key field is empty with the stored hint displayed beside it, and saving without typing preserves the stored keys

#### Scenario: Toggle rejection surfaces as toast
- **WHEN** a Member attempts to toggle a tool
- **THEN** the request is refused with a 403 and the pane surfaces a toast

#### Scenario: Member sees rows without controls
- **WHEN** a Member without tools.write opens the Tools pane
- **THEN** every toggleable row renders read-only — no toggle and no gear — while names, descriptions, and states stay visible

#### Scenario: Invalid config reported inline
- **WHEN** the user saves with a row missing its name, a row missing a key for a key-requiring provider, or two rows with the same name
- **THEN** the dialog shows the validation error on the offending rows and does not close

### Requirement: API keys pane
The keys pane SHALL create keys through a create-key dialog requiring a key name (keys keep a random suffix); the newly created key's full value SHALL be presented once with a one-time copy warning. The pane SHALL offer reveal, clipboard copy, and revoke per key, with creator-symmetric scoping: Members see and manage only the keys they created, while holders of `workspace.write` see and manage every workspace key. When the browser blocks clipboard access the pane SHALL show a danger toast instead of failing silently.

#### Scenario: Create and copy a key
- **WHEN** the user creates a key through the dialog and copies it
- **THEN** the toasts "API key created — copy it now, it won't be shown again" then "Key copied to clipboard" appear in order

#### Scenario: Name required
- **WHEN** the key name field in the create-key dialog is empty
- **THEN** the create action is disabled

#### Scenario: Member scope is own keys
- **WHEN** a Member who exchanged a key opens the keys pane
- **THEN** the list shows that key with reveal, copy, and revoke available, and no other member's keys

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name, timezone, and the workspace default model through the API on explicit save (thread retention remains a local workspace field until its domain integrates; the workspace URL is display-only and shows no plan indicator). The timezone field SHALL be a searchable selector over the full IANA zone list, each entry showing its UTC offset. The default model SHALL be picked provider-first: a provider select over the workspace's configured provider configs, then a model combobox fed by that provider's model-catalog endpoint (free-text when the catalog resolves nothing); the pair persists as the workspace default model, and clearing both removes the default. Editing affordances SHALL be permission-gated: for a viewer without `workspace.write` the name, timezone, and default-model fields render read-only and the save control is hidden, with the shared-memory editor's own gating unchanged. When the server refuses a change (for example clearing the default while inherit-agents exist) the pane SHALL surface the refusal as an error toast naming the reason.

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
- **WHEN** a member without workspace.write opens the workspace pane
- **THEN** the editable fields render read-only and no save control is offered

#### Scenario: Searchable timezone
- **WHEN** the user types "jakar" into the timezone field
- **THEN** "Asia/Jakarta" with its UTC offset is selectable

### Requirement: Providers pane
The providers pane SHALL present the workspace's provider configs from the API under two internal tabs — **Language models** (default) and **Decision** — listing rows with type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. `typesafe` configs SHALL list only under the Decision tab, which SHALL also carry a hint that decision providers power routing calls and never serve chat or agent models; language-model types SHALL list only under Language models. Owners and Admins create and edit configs through a structured dialog (type select whose options are grouped into "Language models" and "Decision", name, base URL shown only when the type requires or overrides it, password-style key input, and the catalog-mapping select for `-compatible` types); the key input is write-only — existing keys are never echoed back, replaced only. Creation, edit, and delete affordances SHALL render only for holders of `providers.write`; Members browse both tabs read-only with no add, edit, toggle, or delete controls. The dialog SHALL carry a "Verify connection" action that tests the currently entered form values against the provider before saving — the typed key for a new config, and the stored key when editing with a blank key field — reporting the outcome inline (busy, success, or the provider error); the outcome is never persisted and does not gate the save button. For a provider type that does not require an API key, the dialog SHALL render a "This endpoint needs no API key" checkbox above the key field (unchecked by default, and not rendered when the type requires a key or a key is stored): checking it SHALL disable the API key field and enable the Verify connection action without a key, and unchecking it SHALL re-enable the field; the checkbox is frontend state only and is never persisted. Selecting the `typesafe` type SHALL show the base URL field with the canonical systemone origin as its placeholder and SHALL NOT render the keyless checkbox. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. Each tab SHALL show an inviting empty state when it has no configs. Model-selection surfaces across the app — the agent config modal, the workspace default-model picker, and the memory pane's provider selects — SHALL list only language-model providers; decision providers SHALL be excluded from every model picker.

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
- **THEN** the pane shows an inviting empty state, visible to Members too (read is a permission)

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

#### Scenario: Member sees read-only providers
- **WHEN** a Member without providers.write opens the providers pane
- **THEN** both tabs list their configs read-only — no add, edit, toggle, or delete control renders

### Requirement: Memory pane
The Memory section SHALL present the workspace memory pane as three tabs — **Configuration**, **Facts**, and **Morning report** — with **Configuration** as the tab in view when the pane opens. The Facts tab SHALL remain the derived-facts browser and the Morning report tab the consolidation report, as built. Mutation affordances SHALL be permission-gated: editing and saving the Configuration tab, promoting or deleting a fact, and running consolidation now SHALL render only for holders of `workspace.write`; Members browse all three tabs read-only (notes and events remain visible per their membership read scope). The Configuration tab SHALL expose the visibility default posture (narrow / org-shared), the ingestion master toggle, the memory side-call model (agent default, or a specific provider+model pair), and the embedding configuration: an embedding provider select over the workspace's configured provider configs, an embedding model field (dropdown over the provider's embedding-classified catalog models when any resolve, free text otherwise), and a **Dimension** dropdown offering "Auto — detect on test" plus OnClaw's supported embedding dimensions — 768, 1024, 1536, 2048, and 3072. Selecting an embedding model whose dimension OnClaw knows SHALL preselect that dimension; "Auto" leaves the dimension unset until the connection test discovers it. Test connection SHALL remain the authority: its discovered dimension fills an Auto selection, and a stored or chosen dimension that mismatches the endpoint's returned dimension SHALL be reported as an error, not silently stored. The Configuration tab SHALL additionally expose a **Decision backend** block: an unchecked-by-default checkbox ("Use decision backend") whose checked state reveals a provider select (over `typesafe` decision providers only) and a model field prefilled with `jev-latest`; unchecked, no decision fields render and the pane stores nothing for them. Saving with the box checked but no provider selected SHALL be blocked with inline validation, mirroring the side-call "Specific model" flow. The block's help copy SHALL state that the decision backend classifies each turn's memory need in place of the side-call model and that entity routing (associative) requires the language model.

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

#### Scenario: Member sees memory read-only
- **WHEN** a Member without workspace.write opens the Memory pane
- **THEN** all three tabs render read-only — no settings save, fact promote/delete, or consolidate-now control

### Requirement: Hooks pane
The workspace settings SHALL offer a Hooks pane listing the workspace's hooks in evaluation order, each row showing name, event, handler type, a summary of what it selects and whether it can block, subscribed scope, health status (healthy, errored with failure count, disabled), and an enable toggle. Mutation affordances SHALL be gated by `hooks.write`: creation, editing, reordering, the enable toggle, and deletion render only for holders, while Members see the list (including the instance-hooks read-only section) without controls. Rows SHALL be reorderable by dragging, and the pane SHALL state that hooks evaluate top to bottom with the first block winning. The pane SHALL offer creation and editing through a dialog with, in order: name; event select (with a description of when it fires and whether it can block); a single applies-to field read as a matcher string — empty or `*` selecting every occurrence, exact names and dotted families split on comma, pipe, or whitespace, and anything else an unanchored regular expression — with a live count of how many available values it matches; an optional `if` input-gate field on tool events in `ToolName(pattern)` form; a handler section that swaps by handler type (URL and masked secret headers for webhooks; program, argument rows, and masked secret environment variables for commands; server, tool, and structured input rows with placeholder hints for MCP tools; policy prompt, explicit provider/model pickers, and per-run cap for evaluators; a JavaScript code editor for scripts); a timeout; a failure policy radio (allow default); and an enabled toggle. Saving SHALL surface validation errors per field, including matcher/regex errors, the match count, and script syntax errors with their position. The pane SHALL surface instance-level hooks that reach the workspace in a read-only section, distinguishable from workspace hooks.

#### Scenario: Editing a hook round-trips its selection
- **WHEN** a workspace administrator reopens a hook saved with the matcher string `web.*`
- **THEN** the dialog shows the same string with its live match count

#### Scenario: Health is visible at a glance
- **WHEN** a hook's last delivery attempts failed
- **THEN** its row shows an errored status with the failure count, linking to its execution history

#### Scenario: Member sees hooks read-only
- **WHEN** a Member without hooks.write opens the Hooks pane
- **THEN** the workspace and instance hook lists render without create, edit, reorder, toggle, or delete controls
