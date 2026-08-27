# web-app/settings Specification

## Purpose

The workspace settings modal and its seven panes — workspace, members & roles, integrations, MCP servers, skills, API keys, and notifications — including the workspace danger zone.

## Requirements

### Requirement: Settings navigation
Settings SHALL present seven sections — Workspace, Members & roles, Integrations, MCP servers, Skills, API keys, Notifications — as a tabbed rail inside one modal, each pane reachable without reloading.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the tab rail
- **THEN** the keys pane renders with its manage controls

### Requirement: Workspace pane and danger zone
The workspace pane SHALL edit workspace name, timezone, default model, and thread retention (the workspace URL is display-only), applying changes on explicit save. Deleting the workspace SHALL require a second confirming click within a 4-second window, SHALL be refused when it is the only remaining workspace, and on success SHALL switch the app to another workspace.

#### Scenario: Two-click delete
- **WHEN** the user clicks "Delete workspace" and does not click again within 4 seconds
- **THEN** nothing is deleted and the button returns to its initial label

#### Scenario: Last workspace guard
- **WHEN** the user attempts to delete the only workspace
- **THEN** deletion is refused with a danger toast

### Requirement: Members pane
The members pane SHALL let an Owner invite by email with a role (Admin or Member, rejecting values without `@`), change non-owner roles inline, and remove non-owner members. The owner row SHALL be immutable and labeled "You are the owner".

#### Scenario: Invite validation
- **WHEN** the invite email field contains "not-an-email"
- **THEN** the Invite button is disabled

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
