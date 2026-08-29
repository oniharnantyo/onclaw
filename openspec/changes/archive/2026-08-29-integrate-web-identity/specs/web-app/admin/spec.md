## Purpose

Instance administration in the web app: when the active workspace is the master tenant, qualified members get an Admin area for managing tenants, users, and superadmins — regular screens backed by the admin API, styled per the design contract.

## ADDED Requirements

### Requirement: Admin entry gating
When the active workspace is the master tenant and the member holds `admin.*` permissions, the navigation SHALL gain an Admin entry linking to /admin. No admin UI SHALL be visible or reachable for other members or workspaces; direct /admin visits render a not-authorized state. The server remains authoritative — 403/404 responses render as error states.

#### Scenario: Visible in master only
- **WHEN** a qualified member is in the master workspace
- **THEN** the Admin entry is visible and leads to /admin

#### Scenario: Hidden otherwise
- **WHEN** anyone else opens /admin directly
- **THEN** a not-authorized state renders (client gate or server 403/404 state)

### Requirement: Tenants screen
Admin → Tenants SHALL list every workspace: name, slug, member count, status (Active/Suspended). Suspend/restore actions (master protected), and a create-tenant modal (name, slug, timezone, owner email — new email auto-creates a passwordless account, existing email assigns that user) with per-field errors and toasts.

#### Scenario: Suspend and restore
- **WHEN** a superadmin suspends then restores workspace "acme"
- **THEN** the row flips Suspended→Active with toasts, and its members saw a suspended-state screen while suspended

#### Scenario: Suspend master
- **WHEN** suspend is attempted on the master row
- **THEN** the action is unavailable or rejected with 400; nothing changes

#### Scenario: Create tenant with owner
- **WHEN** a superadmin creates a tenant with a new owner email
- **THEN** the tenant appears Active, the new owner is Owner, and the owner account was created

### Requirement: Users screen
Admin → Users SHALL list all accounts (email, name, avatar, status, membership count), create users (email, name, password), and disable/enable globally.

#### Scenario: Global disable
- **WHEN** a superadmin disables a user
- **THEN** the row shows Disabled and the user is ejected from all workspaces on next request

#### Scenario: Create user
- **WHEN** a superadmin creates a user with email/name/password
- **THEN** the users table gains the row with a toast

### Requirement: Superadmins screen
Admin → Superadmins SHALL list master-tenant Superadmin members and support promote (grant) and demote (revoke). Demoting the last superadmin SHALL surface the server's `last_owner_protected` guard as a toast and change nothing.

#### Scenario: Promote and guard
- **WHEN** a superadmin promotes a master member, then attempts to demote the last remaining superadmin
- **THEN** promotion applies immediately; the last demotion toasts the guard and the member keeps the role
