## MODIFIED Requirements

### Requirement: Admin entry gating
When the active workspace is the master tenant and the member holds `admin.*` permissions, the navigation SHALL gain a **Workspaces** entry (tenant management) and an **Accounts** entry (user management) linking to `/admin/workspaces` and `/admin/accounts`. No admin UI SHALL be visible or reachable for other members or workspaces; direct visits render a not-authorized state. The server remains authoritative — 403/404 responses render as error states.

#### Scenario: Visible in master only
- **WHEN** a qualified member is in the master workspace
- **THEN** the Workspaces and Accounts entries are visible and lead to their screens

#### Scenario: Hidden otherwise
- **WHEN** anyone else opens /admin/* directly
- **THEN** a not-authorized state renders (client gate or server 403/404 state)

### Requirement: Tenants screen
Admin → Workspaces SHALL list every workspace: name, slug, member count, status (Active/Suspended). Rows offer suspend/restore (master protected), an edit modal (rename, timezone, ownership transfer, member management), and a create-tenant flow: name, URL slug, timezone picker, and an owner chosen from a searchable list of existing users — accounts are created in Accounts first, not inline. Per-field errors and toasts throughout.

#### Scenario: Suspend and restore
- **WHEN** a superadmin suspends then restores workspace "acme"
- **THEN** the row flips Suspended→Active with toasts, and its members saw a suspended-state screen while suspended

#### Scenario: Suspend master
- **WHEN** suspend is attempted on the master row
- **THEN** the action is unavailable or rejected with 400; nothing changes

#### Scenario: Create tenant with owner
- **WHEN** a superadmin creates a tenant, picking owner "Dana (dana@acme.dev)" from the user picker
- **THEN** the tenant appears Active with Dana as Owner and a toast confirms

#### Scenario: Edit tenant fields
- **WHEN** a superadmin edits the tenant's name to "Acme Corporation" and timezone to "Asia/Jakarta" and saves
- **THEN** the list row reflects the new values after save

#### Scenario: Transfer ownership
- **WHEN** a superadmin changes the tenant's owner to any user via the edit modal
- **THEN** the transfer applies: the new owner is Owner, the previous owner becomes Admin, and the members list in the modal updates

#### Scenario: Add member from edit modal
- **WHEN** a superadmin adds an existing user as Admin via the edit modal
- **THEN** the modal's member list gains the row with the Admin badge

### Requirement: Users screen
Admin → Accounts SHALL list all accounts: email, name, avatar, status, membership count, and a superadmin badge. Superadmin state is managed inline — promote (grant) and demote (revoke) actions per row; demoting the last superadmin SHALL surface the server's `last_owner_protected` guard as a toast and change nothing. Creating users (email, name, password) and global disable/enable round out the screen.

#### Scenario: Global disable
- **WHEN** a superadmin disables a user
- **THEN** the row shows Disabled and the user is ejected from all workspaces on next request

#### Scenario: Create user
- **WHEN** a superadmin creates a user with email/name/password
- **THEN** the users table gains the row with a toast

#### Scenario: Badge and inline promote
- **WHEN** a superadmin promotes a non-superadmin user
- **THEN** the row's superadmin badge appears immediately

#### Scenario: Last superadmin protected
- **WHEN** the only superadmin is demoted
- **THEN** a toast surfaces `last_owner_protected` and the badge remains

## REMOVED Requirements

### Requirement: Superadmins screen
**Reason**: Superadmin management merges into the Users screen — superadmin is a property of a user (badged inline with promote/demote actions), not a separate roster worth its own tab.
**Migration**: Promote/demote live on Users rows (same grant/revoke API); the standalone Superadmins tab and pane are removed.
