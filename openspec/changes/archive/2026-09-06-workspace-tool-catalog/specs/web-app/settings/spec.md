# web-app/settings Delta

## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the nine sections — Workspace, Providers, Members & roles, Integrations, MCP servers, Skills, Tools, API keys, Notifications — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the section nav
- **THEN** the keys section renders with its manage controls and the URL reads /settings/keys

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
- **THEN** the workspace sidebar is not rendered; the section nav column and section content fill the area

## ADDED Requirements

### Requirement: Tools pane (API-backed)
The Tools pane SHALL list every tool from the workspace tools endpoint as a flat list — one row per tool with its display name, one-line description, and an enable toggle reflecting (and driving) the workspace-wide tool status; toggling requires the settings-management permission and SHALL surface guard rejections as toasts. Configurable tools SHALL additionally render a gear button opening a structured config dialog rendered from the tool's config-field schema: one labeled control per field — `secret` fields as password-style write-only inputs (existing values shown only as hints, replaced on save), `text` as inputs, `number` as numeric inputs, `boolean` as toggles, `enum` as selects fed by the schema's options — with help text and a Save action. A configurable tool whose required config is missing SHALL show its toggle as unavailable until configured, and the dialog SHALL state what is missing; saving valid config SHALL enable toggling. The pane SHALL NOT offer raw JSON editing for any structured value.

#### Scenario: Flat list with toggles
- **WHEN** a Member opens the Tools pane
- **THEN** every catalog tool appears as a row — display name, description, toggle — with no group headings, driven by the workspace tools endpoint

#### Scenario: Gear opens config dialog
- **WHEN** the user activates the gear on "Web Search"
- **THEN** a dialog opens with a provider select (registry options with their credential kinds) and an API key field, one labeled control per property

#### Scenario: Browser config dialog
- **WHEN** the user opens the Browser config dialog
- **THEN** it exposes headless toggle, remote CDP URL, max pages, idle timeout, and action timeout, one labeled control per property, with help text explaining that a set CDP URL makes headless irrelevant

#### Scenario: Disabled until configured
- **WHEN** no search provider is configured for the workspace
- **THEN** the Web Search row shows its toggle as unavailable with an explanatory hint, and saving a valid provider config enables the toggle

#### Scenario: Secret never echoed
- **WHEN** the user reopens the Web Search dialog after saving an API key
- **THEN** the key field is empty with the stored hint displayed beside it, and saving without typing preserves the stored key

#### Scenario: Toggle rejection surfaces as toast
- **WHEN** a Member attempts to toggle a tool
- **THEN** the request is refused with a 403 and the pane surfaces a toast

#### Scenario: Invalid config reported inline
- **WHEN** the user saves the Browser dialog with max pages 0
- **THEN** the dialog shows the validation error on that field and does not close
