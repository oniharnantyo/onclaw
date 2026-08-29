## ADDED Requirements

### Requirement: Workspace pane (API-backed)
The workspace pane SHALL edit workspace name and timezone through the API on explicit save (default model and thread retention remain local workspace fields until their domains integrate; the workspace URL is display-only).

#### Scenario: Save settings
- **WHEN** a member with workspace.write saves a new workspace name or timezone
- **THEN** the change persists (reload keeps it) and a toast confirms

#### Scenario: Save without permission
- **WHEN** a member without workspace.write attempts to save
- **THEN** the save action is unavailable (or rejected with a forbidden toast)

## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Workspace pane and danger zone
**Reason**: Workspace deletion is removed from the product (no backend endpoint); the danger zone becomes "Leave workspace", and the pane's save path moves to the API.
**Migration**: See the ADDED "Workspace pane (API-backed)" and "Danger zone: leave workspace" requirements (leave lives in the workspaces delta), plus Admin → Tenants suspend/restore for instance-side tenant removal.
