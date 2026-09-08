# web-app/settings Specification

## Purpose

The workspace settings routed page and its nine sections — workspace, providers, members & roles, integrations, MCP servers, skills, tools, API keys, and notifications.

## Requirements

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

### Requirement: Members pane
The members pane SHALL render members from the members API — email, name, avatar (avatar_url image with initials fallback), role, joined date. Owners and Admins add members through an invite dialog — email plus a role selected from the workspace's real roles (fetched once per workspace) — change roles inline, and remove members through the API; guard rejections (peers unmanageable, last_owner_protected, missing permission) surface as toasts. Password-less members (added by email without a password) show an "Invited" hint; the member's own row is labeled "You".

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

### Requirement: MCP servers pane
The MCP pane SHALL render from the workspace MCP endpoints (API-backed, no local mock state): one card per registered server showing its name, transport, status dot (Connected/Paused/Error), exposed-tool count, and the number and names of agents whose `enabled_mcps` reference it. Adding and editing SHALL happen through a structured dialog — never free-text config entry — with one labeled control per property: server name; transport select (`stdio`, `streamable_http`, `sse`); for stdio a command input, an args input, and an env-var row editor (name plus write-only value showing the stored hint); for streamable HTTP and SSE a URL input and a header row editor (name plus write-only value showing the stored hint). Submitting the dialog SHALL create or update via the API and surface the probe result — status and error message — so a misconfigured server is visible immediately. Pausing/resuming SHALL toggle the server's master switch via the API; errored servers SHALL offer a retry that triggers a re-probe; deletion SHALL ask for confirmation. Expanding a server SHALL list its exposed tool names as read-only chips. Destructive or write actions SHALL be limited to `tools.write` holders; read-only members see the pane without them.

#### Scenario: Pause with dependents
- **WHEN** a connected server referenced by two agents' `enabled_mcps` is paused
- **THEN** its status becomes Paused, its row dims, and the usage note flags the referencing agents as losing its tools

#### Scenario: Add server via dialog
- **WHEN** the user submits the add dialog for a `stdio` server — name "GitHub", command `npx`, args, and one env var
- **THEN** the server is created via the API, appears in the list with its probe status (Connected or Error with the message), and the dialog closes

#### Scenario: Add HTTP server with secret header
- **WHEN** the user adds a `streamable_http` server with an Authorization header value
- **THEN** the value is submitted once and the dialog afterwards shows only its stored hint — never the value

#### Scenario: Edit server
- **WHEN** the user opens edit on a server, changes its transport through the select (reconfiguring the form), and saves
- **THEN** the row shows the new transport and the fresh probe status after save

#### Scenario: Edit keeps the stored secret
- **WHEN** the user opens edit on a server and saves without retyping a header value
- **THEN** the update sends an empty value for that header and the stored secret is kept

#### Scenario: Transport select reconfigures the form
- **WHEN** the user switches the transport select from `stdio` to `streamable_http`
- **THEN** the command/args/env controls are replaced by URL and header controls

#### Scenario: Failed probe surfaces inline
- **WHEN** a create or edit probe cannot reach the server
- **THEN** the card's status dot turns Error with the failure message visible and no toast-only error

#### Scenario: Retry errored server
- **WHEN** the user clicks retry on an errored server
- **THEN** a re-probe is requested via the API and the status returns to Connected or stays Error with a refreshed message

#### Scenario: Delete asks for confirmation
- **WHEN** the user deletes a server referenced by agents
- **THEN** a confirmation names the referencing agents before the delete is sent

#### Scenario: Member sees a read-only pane
- **WHEN** a holder without `tools.write` opens the pane
- **THEN** server cards and tool lists render, but add/edit/pause/retry/delete controls are absent
### Requirement: Skills pane
The skills pane SHALL present the workspace skill library backed by the skills API: each row shows the skill's name, version chip, source badge (`authored` | `upload` | `git` | `fork`), dependency status chip when unmet, an enable master toggle, an edit action, and an uninstall action (confirm dialog). An **Install skill** wizard SHALL walk through source selection (Author / Upload / Git or URL; Fork when entered from a system skill), content (body editor for Author; archive drop with file-tree preview for Upload; URL with ref and optional token plus discovered-skill selection for Git/URL), and a dependency review step listing every declared or inferred dependency with its resolution status — tools (with the pre-checked "enable everywhere" option), binaries (per-platform install command with copy action and re-check), python packages (auto-provision checkbox). Installing with unmet dependencies SHALL be allowed and leave a persistent warning chip on the row. Disabling a skill SHALL toast that it was removed from every agent; enabling SHALL restore it everywhere. A **System skills** section SHALL list embedded skills as read-only locked entries marked always-on with a Fork-to-workspace action. Holders of `skills.read` without `skills.write` (Members) SHALL see the same lists with no action affordances.

#### Scenario: Install custom skill
- **WHEN** the user authors "Changelog sweeper" with a SKILL.md body and installs through the wizard
- **THEN** it appears with version `0.1.0`, an `authored` badge, `enabled` on, and a toast that it is live on all agents

#### Scenario: Edit custom skill
- **WHEN** the user edits "Changelog sweeper" and changes its description
- **THEN** the new description renders on the row after save

#### Scenario: Dependency review reports a missing binary
- **WHEN** the wizard's review step evaluates a skill declaring `pdftotext` absent from the server PATH
- **THEN** the step shows the binary as unmet with the per-platform install command, a copy button, and a re-check action, and install completes with a warning chip on the row

#### Scenario: Enable-everywhere option
- **WHEN** the user installs a skill declaring the `web.search` tool dependency with the pre-checked enable option left on
- **THEN** the skill installs and the agent wizard's tool chips show Web Search enabled for every agent

#### Scenario: Master toggle disables everywhere
- **WHEN** the user toggles "Changelog sweeper" off
- **THEN** the toast reads the skill was disabled and removed from every agent, and the row renders dimmed until re-enabled

#### Scenario: Upload with entry-point error
- **WHEN** the user drops a zip without a root SKILL.md
- **THEN** the content step shows the error state and nothing is installed

#### Scenario: Multi-skill repo selection
- **WHEN** a git URL resolves to a tree containing three skills
- **THEN** the wizard lists all three with descriptions and installs only the checked ones

#### Scenario: System skills locked with fork
- **WHEN** the user opens the System skills section
- **THEN** each entry renders locked and always-on, and its Fork action creates a workspace copy (`fork` badge) opened for editing

#### Scenario: Member sees read-only inventory
- **WHEN** a Member-role holder opens the Skills pane
- **THEN** the library and system lists render with no install, toggle, edit, or uninstall affordances

### Requirement: Tools pane (API-backed)
The Tools pane SHALL list every tool from the workspace tools endpoint as a flat list — one row per tool with its display name, one-line description, and an enable toggle reflecting (and driving) the workspace-wide tool status; toggling requires the settings-management permission and SHALL surface guard rejections as toasts. Configurable tools SHALL additionally render a gear button opening a structured config dialog rendered from the tool's config-field schema: one labeled control per field — `secret` fields as password-style write-only inputs (existing values shown only as hints, replaced on save), `text` as inputs, `number` as numeric inputs, `boolean` as toggles, `enum` as selects fed by the schema's options — with help text and a Save action. For `web.search` the dialog SHALL additionally render a provider-stack list editor: one row per configured entry with a reorder control pair (disabled at the list bounds), a name input, a provider select fed by the registry options, the credential field that provider requires (a password-style write-only key input showing the stored last-4 hint, or a base-URL input for SearXNG), and a remove action; an Add control appends a new row, and a timeout field bounds the per-attempt request timeout. Rows in the first three positions SHALL be labeled "in rotation"; rows below SHALL render dimmed and labeled "standby", and reordering SHALL re-evaluate the labels immediately. Removing a row SHALL not persist until Save; saving SHALL submit the whole ordered list with entry ids, and a row whose credential field is left empty SHALL keep its stored credential (reorder-safe via the stable entry id). Validation failures SHALL render inline per row (empty name, missing credential for a key-requiring provider, duplicate names) and block Save. The pane SHALL NOT offer raw JSON editing for any structured value.

#### Scenario: Flat list with toggles
- **WHEN** a Member opens the Tools pane
- **THEN** every catalog tool appears as a row — display name, description, toggle — with no group headings, driven by the workspace tools endpoint

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

#### Scenario: Invalid config reported inline
- **WHEN** the user saves with a row missing its name, a row missing a key for a key-requiring provider, or two rows with the same name
- **THEN** the dialog shows the validation error on the offending rows and does not close
### Requirement: API keys pane
The keys pane SHALL create keys through a create-key dialog requiring a key name (keys keep a random suffix); the newly created key's full value SHALL be presented once with a one-time copy warning. The pane SHALL offer reveal, clipboard copy, and revoke per key. When the browser blocks clipboard access the pane SHALL show a danger toast instead of failing silently.

#### Scenario: Create and copy a key
- **WHEN** the user creates a key through the dialog and copies it
- **THEN** the toasts "API key created — copy it now, it won't be shown again" then "Key copied to clipboard" appear in order

#### Scenario: Name required
- **WHEN** the key name field in the create-key dialog is empty
- **THEN** the create action is disabled

### Requirement: Notifications pane
The notifications pane SHALL offer toggles for cron failures, agent errors, and a weekly digest, plus a routing email field, held as a local draft until the user saves.

#### Scenario: Draft until save
- **WHEN** the user toggles "Weekly digest" but leaves the pane without saving
- **THEN** the workspace notification settings are unchanged

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name and timezone through the API on explicit save (default model and thread retention remain local workspace fields until their domains integrate; the workspace URL is display-only and shows no plan indicator). The timezone field SHALL be a searchable selector over the full IANA zone list, each entry showing its UTC offset.

#### Scenario: Save settings
- **WHEN** a member with workspace.write saves a new workspace name or timezone
- **THEN** the change persists (reload keeps it) and a toast confirms

#### Scenario: Save without permission
- **WHEN** a member without workspace.write attempts to save
- **THEN** the save action is unavailable (or rejected with a forbidden toast)

#### Scenario: Searchable timezone
- **WHEN** the user types "jakar" into the timezone field
- **THEN** "Asia/Jakarta" with its UTC offset is selectable

### Requirement: Providers pane
The providers pane SHALL list the workspace's provider configs from the API — type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. Owners and Admins create and edit configs through a structured dialog (type select from the six catalog types, name, base URL shown only when the type requires or overrides it, password-style key input); the key input is write-only — existing keys are never echoed back, replaced only. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. The pane SHALL show an inviting empty state when no providers are configured.

#### Scenario: Create an OpenAI config
- **WHEN** an Owner submits type "OpenAI", name "Acme prod", key through the create dialog
- **THEN** the row appears with type badge OpenAI, key set "…last4", enabled on, and a creation toast

#### Scenario: Compatible type form
- **WHEN** the user selects type "openai-compatible" in the create dialog
- **THEN** the base URL field becomes required and help text explains the origin rule (no version path)

#### Scenario: Key never echoed
- **WHEN** the user edits "Acme prod" without touching the key field
- **THEN** the form never displays the stored key, and PATCH preserves it

#### Scenario: Verify transient result
- **WHEN** the user clicks verify on "Acme prod"
- **THEN** a busy state shows, then an inline banner reports success or the provider error; the row data is unchanged

#### Scenario: Empty state
- **WHEN** the workspace has no provider configs
- **THEN** the pane shows an empty state inviting configuration, visible to Members too (read is a permission)

### Requirement: Workspace memory editor
The Workspace pane SHALL include a shared-memory (`WORKSPACE.md`) editor below the existing workspace fields: a free-form textarea (memory is unstructured markdown), a live size/token counter fed by the server cap, and save via the workspace memory endpoint. The editor SHALL load for all members (read state) and save SHALL follow the workspace settings-management permission — Members see the editor disabled or read-only, Owner/Admin persist changes. Over-cap saves SHALL surface the 422 field error inline. The editor SHALL NOT participate in the general workspace-details save (renaming the workspace must not rewrite memory and vice versa).

#### Scenario: Admin edits shared memory
- **WHEN** an Owner or Admin opens Settings → Workspace, edits the memory textarea, and saves
- **THEN** the content persists via the workspace memory endpoint without altering the workspace's other fields

#### Scenario: Member sees read-only
- **WHEN** a Member opens the Workspace pane
- **THEN** the memory editor is visible read-only and its save control is disabled or hidden

#### Scenario: Rename does not touch memory
- **WHEN** an Admin saves a workspace rename from the pane
- **THEN** the stored memory content is unchanged
