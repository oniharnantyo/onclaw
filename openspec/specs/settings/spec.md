# settings Specification

## Purpose

Provide the settings menu with panes for providers, skills, tools, hooks, and MCP servers — the shared configuration that agents draw on — ported from onclaw-v2's settings with the tenancy-related panes removed.

## Requirements

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

### Requirement: Skills pane

The settings menu SHALL list skills discovered from the skills directory, showing each skill's name and description, and SHALL allow enabling or disabling a skill globally. Disabled skills MUST NOT be offered to agents.

#### Scenario: Disable a skill

- **WHEN** the user disables a skill
- **THEN** that skill no longer appears as selectable in agent configuration or the composer's skill menu

### Requirement: Tools pane

The settings menu SHALL list the available tools with their descriptions and SHALL allow enabling or disabling each tool. Disabled tools MUST NOT be offered to agents. The pane SHALL also configure the web_search provider chain: providers are chosen from the catalog (brave, exa, perplexity, and firecrawl, each requiring an API key; searxng requiring a base URL), ordered into a failover chain, and persisted for the runtime. Saving a chain provider without its required credential SHALL be rejected.

#### Scenario: Toggle a tool

- **WHEN** the user disables a tool that an agent had enabled
- **THEN** the agent's subsequent turns no longer offer that tool

#### Scenario: Configure the search chain

- **WHEN** the user adds brave with an API key and then searxng with a base URL
- **THEN** the chain is saved in that order and web_search tries brave before searxng

#### Scenario: Missing credential is rejected

- **WHEN** the user adds a chain provider that requires an API key and submits without one
- **THEN** the form rejects the submission and explains which credential is missing

### Requirement: Hooks pane

The settings menu SHALL let the user view, add, edit, and remove global hooks — each bound to one of the supported events with a command or script handler type (other handler types are not offered) — and SHALL provide a test affordance that exercises the hook with a sample event and reports the outcome. Per-agent hook assignment lives in agent configuration, not here.

#### Scenario: Test a hook

- **WHEN** the user runs the test action on a command hook
- **THEN** the hook executes against a sample event and its outcome (success or failure) is displayed

#### Scenario: Only supported handler types are offered

- **WHEN** the user opens the add/edit hook form
- **THEN** the handler-type choices are command and script only

### Requirement: MCP pane

The settings menu SHALL let the user add, edit, and remove MCP server definitions (transport, command or URL, arguments, environment) and SHALL show each server's live connection state — probed on demand, showing connected with the discovered tool count, failed with the reason, or authorization required. Every enabled server is available to agents by default; agents restrict the set from their own configuration. Each server row SHALL show an enabled toggle, a colored status dot (success for connected, danger for failed, warning for authorization-required, neutral for un-probed) alongside the status chip, and actions to refresh and remove the server; the pane SHALL offer an action that refreshes every enabled server. HTTP and SSE server definitions SHALL offer an Authentication setting of none (static headers) or OAuth. A row in the authorization-required state SHALL offer an Authorize action that starts the authorization flow, and an OAuth-authenticated row SHALL offer Disconnect. Disabling a server SHALL take effect immediately; enabling it SHALL refresh the row.

#### Scenario: Add an MCP server

- **WHEN** the user adds a valid MCP server definition
- **THEN** the server is listed with its connection state and becomes available to every agent that has not restricted its server list

#### Scenario: Row toggle disables and enables

- **WHEN** the user toggles a server row off
- **THEN** the server's live connection is dropped immediately and agents no longer receive its tools, and the row shows its disabled state

- **WHEN** the user toggles a server row on
- **THEN** the row refreshes the server so its connection state becomes visible

#### Scenario: Status dot reflects connection state

- **WHEN** a server row is shown in either theme
- **THEN** the row's dot is green when connected, red when failed, amber when authorization is required, and neutral when un-probed

#### Scenario: Refresh all re-dials every enabled server

- **WHEN** the user invokes the refresh-all action
- **THEN** every enabled server is refreshed and each row updates with its fresh outcome

#### Scenario: Authorize action starts the flow

- **WHEN** the user invokes Authorize on a row in the authorization-required state
- **THEN** the system browser opens the server's authorization page, the row is unaffected until the flow completes, and on completion the row refreshes to the new state

#### Scenario: Authentication setting on HTTP servers

- **WHEN** the user edits an HTTP or SSE server definition
- **THEN** the dialog offers Authentication with none and OAuth choices, and the OAuth choice authorizes through the flow rather than static headers

#### Scenario: Disconnect returns the row to authorization-required

- **WHEN** the user invokes Disconnect on an OAuth-authenticated server
- **THEN** its stored tokens are revoked and cleared and the row shows the authorization-required state with an Authorize action

#### Scenario: Probe shows live state

- **WHEN** the user probes a reachable server
- **THEN** its row shows connected with the discovered tool count

#### Scenario: Failed probe shows the reason

- **WHEN** the user probes a server that cannot be reached
- **THEN** its row shows the failed state with the underlying reason and the stored definition is unchanged

### Requirement: Theme parity

The settings menu SHALL render correctly in both the light and dark daisyUI themes across all panes, forms, and lists.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while in settings
- **THEN** all panes render legibly in the selected theme

### Requirement: Pane header action row

Each settings pane's content SHALL present its title with a top-right action
slot on the same row. Providers SHALL place its primary action ("Add
provider") there; panes without a primary action SHALL render the title
alone. The action row SHALL stay on one line at desktop widths in both
themes.

#### Scenario: Add provider sits beside the pane title

- **WHEN** the user opens the Providers pane
- **THEN** the pane title row shows the title on the left and the "Add
  provider" action on the right

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
