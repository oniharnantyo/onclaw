# web-app/settings Specification

## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the eight sections — Workspace, Providers, Members & roles, Integrations, MCP servers, Skills, API keys, Notifications — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the section nav
- **THEN** the keys section renders with its manage controls and the URL reads /settings/keys

#### Scenario: Providers reachable
- **WHEN** the user selects "Providers" in the section nav
- **THEN** the providers section renders, listing the workspace's provider configs

#### Scenario: Deep link
- **WHEN** a member opens /settings/providers directly
- **THEN** the providers section renders without passing through any other section

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
The MCP pane SHALL list servers with transport string, status (Connected/Paused/Error), exposed-tool count, and the agents referencing each; support adding a server by name plus transport string through an add dialog, editing a server's name and transport through an edit dialog, pausing/reconnecting via toggle, retrying errored servers, and expanding the exposed tool list.

#### Scenario: Pause with dependents
- **WHEN** a connected server referenced by two agents is paused
- **THEN** its status becomes Paused, its row dims, and the usage note flags the referencing agents as inactive

#### Scenario: Add server via dialog
- **WHEN** the user submits the add-server dialog with name and transport
- **THEN** the server appears in the list and the dialog closes

#### Scenario: Edit server
- **WHEN** the user opens edit on a server, changes its transport, and saves
- **THEN** the row shows the new transport after save

#### Scenario: Retry errored server
- **WHEN** the user clicks retry on an errored server
- **THEN** a reconnect is attempted and the status returns to Connected

### Requirement: Skills pane
The skills pane SHALL list installed skills with version, source badge for custom ones, run counts, and referencing-agent counts; support installing a custom skill by name and optional description (versioned `0.1.0`) through an install dialog, editing a custom skill's name and description through an edit dialog, and toggling skills — disabled skills SHALL apply to every agent referencing them.

#### Scenario: Install custom skill
- **WHEN** the user installs "Changelog sweeper" through the install dialog
- **THEN** it appears with version `0.1.0`, a `custom` badge, and zero runs

#### Scenario: Edit custom skill
- **WHEN** the user edits "Changelog sweeper" and changes its description
- **THEN** the new description renders on the row after save

### Requirement: API keys pane
The keys pane SHALL create keys through a create-key dialog requiring a key name (keys keep a random suffix); the newly created key's full value SHALL be presented once with a one-time copy warning. The pane SHALL offer reveal, clipboard copy, and revoke per key. When the browser blocks clipboard access the pane SHALL show a danger toast instead of failing silently.

#### Scenario: Create and copy a key
- **WHEN** the user creates a key through the dialog and copies it
- **THEN** the toasts "API key created — copy it now, it won't be shown again" then "Key copied to clipboard" appear in order

#### Scenario: Name required
- **WHEN** the key name field in the create-key dialog is empty
- **THEN** the create action is disabled

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
