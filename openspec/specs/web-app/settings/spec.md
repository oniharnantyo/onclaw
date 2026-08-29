# web-app/settings Specification

## Purpose

The workspace settings modal and its seven panes — workspace, members & roles, integrations, MCP servers, skills, API keys, and notifications — including the workspace danger zone.

## Requirements

### Requirement: Settings navigation
Settings SHALL present seven sections — Workspace, Members & roles, Integrations, MCP servers, Skills, API keys, Notifications — as a tabbed rail inside one modal, each pane reachable without reloading.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the tab rail
- **THEN** the keys pane renders with its manage controls

### Requirement: Members pane
The members pane SHALL render members from the members API — email, name, avatar (avatar_url image with initials fallback), role, joined date. Owners and Admins add members by email with a role selected from the workspace's real roles (fetched once per workspace), change roles inline, and remove members through the API; guard rejections (peers unmanageable, last_owner_protected, missing permission) surface as toasts. Password-less members (added by email without a password) show an "Invited" hint; the member's own row is labeled "You".

#### Scenario: Invite validation
- **WHEN** the invite email field contains "not-an-email"
- **THEN** the Invite button is disabled

#### Scenario: Guard rejection toast
- **WHEN** an admin attempts to manage a peer admin and the server rejects
- **THEN** a toast explains admins cannot manage each other; nothing changes

#### Scenario: Invited member hint
- **WHEN** a member was added without a password
- **THEN** the row shows an "Invited" hint instead of a login-capable status

### Requirement: MCP servers pane
The MCP pane SHALL list servers with transport string, status (Connected/Paused/Error), exposed-tool count, and the agents referencing each; support adding a server by name plus transport string, pausing/reconnecting via toggle, retrying errored servers, and expanding the exposed tool list.

#### Scenario: Pause with dependents
- **WHEN** a connected server referenced by two agents is paused
- **THEN** its status becomes Paused, its row dims, and the usage note flags the referencing agents as inactive

### Requirement: Skills pane
The skills pane SHALL list installed skills with version, source badge for custom ones, run counts, and referencing-agent counts; support installing a custom skill by name and optional description (versioned `0.1.0`), and toggling skills — disabled skills SHALL apply to every agent referencing them.

#### Scenario: Install custom skill
- **WHEN** the user installs "Changelog sweeper"
- **THEN** it appears with version `0.1.0`, a `custom` badge, and zero runs

### Requirement: API keys pane
The keys pane SHALL create keys with a random suffix, show the full value with a one-time copy warning, and offer reveal, clipboard copy, and revoke per key. When the browser blocks clipboard access the pane SHALL show a danger toast instead of failing silently.

#### Scenario: Create and copy a key
- **WHEN** the user creates a key and copies it
- **THEN** the toast "API key created — copy it now, it won't be shown again" then "Key copied to clipboard" appear in order

### Requirement: Notifications pane
The notifications pane SHALL offer toggles for cron failures, agent errors, and a weekly digest, plus a routing email field, held as a local draft until the user saves.

#### Scenario: Draft until save
- **WHEN** the user toggles "Weekly digest" but leaves the pane without saving
- **THEN** the workspace notification settings are unchanged

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name and timezone through the API on explicit save (default model and thread retention remain local workspace fields until their domains integrate; the workspace URL is display-only).

#### Scenario: Save settings
- **WHEN** a member with workspace.write saves a new workspace name or timezone
- **THEN** the change persists (reload keeps it) and a toast confirms

#### Scenario: Save without permission
- **WHEN** a member without workspace.write attempts to save
- **THEN** the save action is unavailable (or rejected with a forbidden toast)
