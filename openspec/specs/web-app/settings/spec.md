# web-app/settings Specification

## Purpose

The workspace settings routed page and its ten sections — workspace, providers, members & roles, gateways, MCP servers, skills, tools, API keys, notifications, and storage.

## Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the ten sections — Workspace, Providers, Members & roles, Gateways, MCP servers, Skills, Tools, API keys, Notifications, and Storage — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

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
The Tools pane SHALL render non-toggleable (always-on) catalog tools in a "Channel Tool" group section at the top of the pane — a section header, then one row per non-toggleable tool with its display name, one-line description, and an always-on badge where other rows carry the enable toggle ("Always on · channel runs" for Channel Post and Channel History, "Always on · facilitator only" for Close Work Session); these rows SHALL render no toggle and no gear (none is configurable), and the section SHALL reflect the endpoint payload — which reports non-toggleable tools as enabled regardless of stored rows. Every toggleable tool SHALL list as a flat list below the section — one row per tool with its display name, one-line description, and an enable toggle reflecting (and driving) the workspace-wide tool status; toggling requires the settings-management permission and SHALL surface guard rejections as toasts. Configurable tools SHALL additionally render a gear button opening a structured config dialog rendered from the tool's config-field schema: one labeled control per field — `secret` fields as password-style write-only inputs (existing values shown only as hints, replaced on save), `text` as inputs, `number` as numeric inputs, `boolean` as toggles, `enum` as selects fed by the schema's options — with help text and a Save action. For `web.search` the dialog SHALL additionally render a provider-stack list editor: one row per configured entry with a reorder control pair (disabled at the list bounds), a name input, a provider select fed by the registry options, the credential field that provider requires (a password-style write-only key input showing the stored last-4 hint, or a base-URL input for SearXNG), and a remove action; an Add control appends a new row, and a timeout field bounds the per-attempt request timeout. Rows in the first three positions SHALL be labeled "in rotation"; rows below SHALL render dimmed and labeled "standby", and reordering SHALL re-evaluate the labels immediately. Removing a row SHALL not persist until Save; saving SHALL submit the whole ordered list with entry ids, and a row whose credential field is left empty SHALL keep its stored credential (reorder-safe via the stable entry id). Validation failures SHALL render inline per row (empty name, missing credential for a key-requiring provider, duplicate names) and block Save. The pane SHALL NOT offer raw JSON editing for any structured value.

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

### Requirement: Hooks pane
The workspace settings SHALL offer a Hooks pane listing the workspace's hooks in evaluation order, each row showing name, event, handler type, a summary of what it selects and whether it can block, subscribed scope, health status (healthy, errored with failure count, disabled), and an enable toggle. Rows SHALL be reorderable by dragging, and the pane SHALL state that hooks evaluate top to bottom with the first block winning. The pane SHALL offer creation and editing through a dialog with, in order: name; event select (with a description of when it fires and whether it can block); a single applies-to field read as a matcher string — empty or `*` selecting every occurrence, exact names and dotted families split on comma, pipe, or whitespace, and anything else an unanchored regular expression — with a live count of how many available values it matches; an optional `if` input-gate field on tool events in `ToolName(pattern)` form; a handler section that swaps by handler type (URL and masked secret headers for webhooks; program, argument rows, and masked secret environment variables for commands; server, tool, and structured input rows with placeholder hints for MCP tools; policy prompt, explicit provider/model pickers, and per-run cap for evaluators; a JavaScript code editor for scripts); a timeout; a failure policy radio (allow default); and an enabled toggle. Saving SHALL surface validation errors per field, including matcher/regex errors, the match count, and script syntax errors with their position. The pane SHALL surface instance-level hooks that reach the workspace in a read-only section, distinguishable from workspace hooks.

#### Scenario: Editing a hook round-trips its selection
- **WHEN** a workspace administrator reopens a hook saved with the matcher string `web.*`
- **THEN** the dialog shows the same string with its live match count

#### Scenario: Health is visible at a glance
- **WHEN** a hook's last delivery attempts failed
- **THEN** its row shows an errored status with the failure count, linking to its execution history

### Requirement: Hook test panel
The Hooks pane SHALL offer a test action that fires a synthetic event at the hook's current configuration, clearly stating that it really executes the handler and records nothing. The result SHALL show a decision badge, duration, and handler-specific detail — exit code and stderr snippet for commands, HTTP status for webhooks, token usage for evaluators, captured console output for scripts — alongside a read-only preview of the event payload that will be sent.

#### Scenario: Test a command hook
- **WHEN** an administrator tests a command hook that blocks the synthetic event
- **THEN** the panel shows a blocked badge with the exit code, duration, and the stderr reason, and the execution history gains no entry

### Requirement: Hook script editor
The `script` handler's configuration SHALL present a JavaScript code editor with syntax highlighting and line numbers. The editor SHALL surface syntax errors inline at their position while the author types, and a script with a syntax error SHALL NOT be submittable. The editor SHALL offer a Format action that beautifies the script in place without changing its meaning.

#### Scenario: Typo surfaces inline
- **WHEN** the author types a script containing a syntax error
- **THEN** the editor marks the offending position with the parser's message

#### Scenario: Parse error blocks save
- **WHEN** the author submits a hook whose script does not parse
- **THEN** the save is blocked client-side before any request is sent

#### Scenario: Format beautifies
- **WHEN** the author invokes Format
- **THEN** the script is reformatted in place with equivalent code

### Requirement: Hook execution history
The Hooks pane SHALL provide a per-hook execution history view listing time, event, decision, duration, and failure detail for each recorded evaluation, including records preserved from hooks that have since been deleted.

#### Scenario: Inspect a blocked call
- **WHEN** an administrator opens the history for a gate hook that blocked a tool call earlier that day
- **THEN** the corresponding execution is listed with its decision, duration, and the reason recorded

### Requirement: Storage pane
Workspace settings SHALL provide a Storage pane as the last entry of the settings navigation (route `/settings/storage`), visible read-only to Members and editable by Owners/Admins. The pane configures the workspace's blob storage backend — chat attachments are its first consumer — and SHALL show the active backend (Local when unconfigured, with an explanatory default note) and let an Owner/Admin choose between Local and S3-compatible storage. The S3 choice SHALL reveal structured fields — endpoint URL, region, bucket, access key id, secret access key (write-only, masked sentinel for a stored value), and a path-style toggle for MinIO/R2-class stores — each a labeled field, never a raw JSON textarea. Saving S3 configuration SHALL run a connectivity probe first and surface its outcome inline (success toast, or the failure reason with the previous configuration left active). Configuration changes SHALL apply to new uploads only; existing stored files stay readable.

#### Scenario: Default state shows local
- **WHEN** a workspace has no storage configuration
- **THEN** the pane shows Local storage as the active backend with a note that uploads use the instance data directory, and no S3 fields are shown

#### Scenario: S3 fields on driver selection
- **WHEN** an Owner selects "S3-compatible" as the driver
- **THEN** the endpoint, region, bucket, access key, secret, and path-style fields appear as labeled inputs

#### Scenario: Probe-gated save success
- **WHEN** an Owner saves S3 configuration and the connectivity probe passes
- **THEN** a toast confirms the save, the pane shows S3-compatible as active, and subsequent uploads land in the bucket

#### Scenario: Probe-gated save failure
- **WHEN** an Owner saves S3 configuration and the probe fails (e.g. wrong secret)
- **THEN** an inline error shows the probe's failure reason, the save does not take effect, and the previously active backend remains

#### Scenario: Stored secret never echoed
- **WHEN** the pane is reopened after S3 configuration was saved
- **THEN** the secret field shows the masked sentinel, and saving without touching it preserves the stored secret

#### Scenario: Member sees read-only pane
- **WHEN** a Member opens the Storage pane
- **THEN** the configuration is visible but the driver selector and fields are disabled (or the save action is unavailable with a forbidden toast)

#### Scenario: Pane sits last in the settings navigation
- **WHEN** the settings navigation renders
- **THEN** "Storage" appears as its last entry, after Notifications

### Requirement: Model combobox capability icons
The agent configuration model combobox SHALL render capability icons on each model option row — image input, PDF input, reasoning, and tool calling — using a distinct icon per capability (eye, file, brain, wrench respectively). An icon SHALL appear only when the catalog affirmatively supports that capability for the (provider, model); unsupported or unknown capabilities SHALL show no icon rather than a struck-through or dimmed state. Each icon SHALL carry a human-readable tooltip. The icons reuse the catalog data the combobox already loads for context-window autofill; no separate lookup is introduced.

#### Scenario: Icons reflect catalog capabilities
- **WHEN** the model dropdown opens for a provider whose catalog entries carry per-model input modalities, reasoning, and tool-calling flags
- **THEN** each option row shows exactly the icons for the capabilities that model affirmatively supports

#### Scenario: Unknown models stay quiet
- **WHEN** the dropdown includes a model with no catalog entry
- **THEN** that row shows the model name with no capability icons and no placeholder markers

### Requirement: Gateways pane
The Gateways pane SHALL present gateway platforms through a second sidebar inside the pane: a section per platform (Telegram, WhatsApp) holding one status-bearing row per gateway account, with the selected account's configuration rendered in a detail pane to the right. Each row SHALL show the platform icon, the account's identity second line (@bot_username for Telegram; the linked lane identity for WhatsApp), and a status dot drawn from a shared vocabulary — Connected, Paused, Error, Not set up (WhatsApp linked state: Linked) — kept in sync with the detail pane's status card. Each platform section SHALL offer an add affordance ("Add a bot" / "Add an account") launching the connect wizard. At widths <768px the sidebar SHALL collapse into a horizontal scrollable chip row carrying the same dots, with the detail stacked beneath; the 360×800 minimum viewport SHALL NOT overflow horizontally.

The pane SHALL widen from the single `max-w-xl` column to the full settings content area (sidebar ~208px + flexing detail). Account selection SHALL be local component state; sections SHALL NOT add URL routes. The connect wizard SHALL require choosing the workspace agent the new account speaks for before it can be saved. The detail pane SHALL be per account: token (write-only, stored encrypted, shown back as a secret hint, replaceable), resolved bot username, bound agent (changeable via the agent picker), transport mode, enable/disable, status, and test message. For a WhatsApp account the detail SHALL open with a lane selection followed by lane-appropriate configuration as labeled fields (never a raw JSON textarea): the cloud lane presents access token, phone number id, app secret, and webhook verify token as write-only fields, plus the webhook callback URL and verify token to paste into the Meta dashboard and a connected status; the multi-device lane presents the pairing flow — start pairing, scan the displayed QR code or enter the displayed 8-digit pair code, live connection status, and logout — preceded by a clear account-ban risk notice. All existing management surfaces persist: bound Telegram groups (bind by in-chat binding command addressed to the owning bot, list with the bound agent and owning bot, unlink). Member-facing, each platform's detail exposes the personal pairing flow: generate a one-time pairing token shown as a copyable command with an expiry countdown, and show/unlink the current platform-wide link for the signed-in member. Guard rejections (non-admin on admin actions, invalid tokens, one-binding-per-group conflicts) surface as toasts. The pane SHALL replace the mock Telegram entry in the legacy integrations list with the real surface.

#### Scenario: Sidebar lists one row per account
- **WHEN** an admin opens the Gateways pane with two Telegram bots and one linked WhatsApp account
- **THEN** the Telegram section lists two rows (one per bot, each with its dot and @username) and the WhatsApp section lists one Linked row

#### Scenario: Add affordance starts the wizard
- **WHEN** an admin selects "Add a bot" in the Telegram section
- **THEN** the connect wizard opens and requires both a bot token and a workspace agent before it can save

#### Scenario: Agent step is mandatory
- **WHEN** the admin attempts to complete the connect wizard without choosing an agent
- **THEN** the wizard refuses to save and names the agent field

#### Scenario: Per-account detail and status
- **WHEN** the admin selects one of the two Telegram rows
- **THEN** the detail pane shows that account's token hint, bound agent, transport, enable state, and status, while the other account keeps running

#### Scenario: Sidebar collapses on small viewports
- **WHEN** the viewport is narrower than 768px
- **THEN** the account rows render as a horizontal scrollable chip row above the stacked detail, carrying the same status dots, with no horizontal page overflow at 360px

#### Scenario: Status reflects a paused account
- **WHEN** a configured account is disabled
- **THEN** its sidebar row dot reads Paused and the detail status card agrees

#### Scenario: Admin connects a bot
- **WHEN** an Owner completes the wizard with a valid BotFather token and a chosen agent
- **THEN** the pane shows the new account row as connected with the bot username resolved from Telegram, and the token is never displayed again

#### Scenario: Admin connects the WhatsApp cloud lane
- **WHEN** an Owner selects the cloud lane for a new WhatsApp account, fills the labeled credential fields, and saves
- **THEN** the account row shows as connected and the pane presents the webhook callback URL and verify token to paste into the Meta dashboard

#### Scenario: Admin pairs a WhatsApp device
- **WHEN** an Owner selects the multi-device lane for a WhatsApp account and starts pairing
- **THEN** the pane shows the ban-risk notice, a QR code with an 8-digit pair code alternative, and a live connection status that settles to connected after the scan

#### Scenario: Default agent picker
- **WHEN** an admin opens the agent control on a gateway account's detail pane
- **THEN** the workspace's real agents are offered, and the saved choice pins that bot's direct messages to that agent

#### Scenario: Group binding listed
- **WHEN** a bound group's binding command is confirmed in Telegram
- **THEN** the pane lists that group with its bound agent and the owning bot, plus an unlink control

#### Scenario: Member pairs from the pane
- **WHEN** a signed-in member opens the pairing flow
- **THEN** a one-time pairing command is shown with a live expiry countdown, and after pairing completes the pane shows their linked identity with an unlink control covering all workspace bots

#### Scenario: Non-admin sees read-only
- **WHEN** a Member opens the Gateways pane
- **THEN** the admin controls (account management, agent binding, transport, bindings, enable/disable) are hidden or disabled, and only the personal pairing flow is offered
