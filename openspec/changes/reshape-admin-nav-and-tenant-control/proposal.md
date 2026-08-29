## Why

The instance admin area conflates tenant management with identity management, its icon-only rail entry is undiscoverable, and tenants cannot be edited after creation — there is no owner transfer, no way for a superadmin to add members to a tenant they aren't in, and no rename/timezone editing. Meanwhile the web still carries prototype-era fiction (Free/Pro plans that don't exist in the backend), two competing workspace-creation flows, hardcoded 5-entry timezone lists, and an onboarding bouncer that hijacks the Agents menu in agent-less workspaces, making the primary nav feel broken.

## What Changes

- **Two gated nav entries instead of one**: the rail's "Admin" item becomes **Workspaces** (tenants-only screen) and **Accounts** (users screen), both visible only for master-tenant members with `admin.*` permissions.
- **Tenant editing (full-stack)**: new admin endpoints let a superadmin rename a tenant, change its timezone, transfer ownership (any user, auto-added if not a member; atomic swap guarantees exactly one owner), and add members with Admin/Member roles. The Tenants screen gains an edit modal.
- **Users absorb Superadmins**: `GET /admin/users` gains an `is_superadmin` flag; the Accounts screen shows a Superadmin badge with inline promote/demote; the separate Superadmins tab is removed.
- **Searchable pickers**: a shared combobox primitive powers a full-IANA timezone picker (via `Intl.supportedValuesOf('timeZone')`) and a user picker for the create-tenant owner field (replacing free-text email + auto-create in the UI flow).
- **Creation consolidated**: the workspace switcher's "Create workspace" entry and `CreateWorkspaceModal` are removed; tenants are created from the Tenants screen only. The backend self-service endpoint remains (unused by web).
- **Plan fiction removed**: all Free/Pro UI, the dead Free/Pro picker in the deleted modal, `plan` on the frontend `Workspace` type, and seed data lose it.
- **Rail usability**: tooltips on hover/focus for icon-only buttons, plus a persisted expand/collapse toggle that reveals text labels (default collapsed to preserve prototype visual parity; toggle hidden on mobile where the drawer provides text nav).
- **Active-state fixes**: the onboarding bouncer no longer hijacks `/agents`, `/cron`, `/runs`; `/welcome` highlights nothing in the rail; the sidebar's dead "Instance Admin" button is removed.

## Capabilities

### New Capabilities

- *(none)*

### Modified Capabilities

- `instance-admin`: tenant management extends to edit (rename/timezone), owner transfer (single-owner swap, auto-add non-members), and tenant member management (list/add by admin); admin users list gains `is_superadmin`.
- `web-app/admin`: admin area splits into Workspaces (tenants-only) and Accounts (users with merged superadmin management); create-tenant owner field becomes a searchable user picker; new edit-tenant modal.
- `web-app/shell`: rail gains Workspaces/Accounts entries, tooltips, persisted expand/collapse; active-state rules fixed (no bouncer on non-chat routes, no highlight on /welcome); sidebar admin button removed.
- `web-app/workspaces`: workspace switcher becomes membership-only (no creation); tenant creation is admin-provisioned.
- `web-app/settings`: plan chip removed; workspace timezone field uses the searchable combobox.

## Impact

- **Backend**: `internal/server/handlers/admin_workspaces.go` (update/owner/members endpoints), `admin_users.go` (`is_superadmin` on list), `internal/server/router.go` (routes + guards), store `Members` port gains tenant-member listing reused from existing methods; no DB migrations (ownership remains a role on the members table; the swap updates role assignments in one transaction).
- **Web**: `Rail.tsx`, `Sidebar.tsx` (button removal), new `Tooltip` + `Combobox`/`TimezoneSelect`/`UserPicker` primitives, `AdminView` split into Workspaces/Users screens, `TenantsPane` + `EditTenantModal`, `UsersPane` (superadmin column), `CreateTenantModal` (user picker), removal of `CreateWorkspaceModal` + `createWsOpen`/`createWorkspace` store pieces, `api.ts` admin namespace.
- **Tests**: Go handler/router tests for new endpoints and guards; vitest updates for `AdminView`, `TenantsPane`, `UsersPane`, `Rail`, removal of `CreateWorkspaceModal.test.tsx`; Playwright visual-parity suite unaffected by default (collapsed rail); manual smoke via `scripts/smoke.sh` for admin flows.
- **Non-goals**: backend self-service `POST /api/v1/workspaces` stays callable (web stops using it); the API-level auto-create-on-unknown-email behavior is unchanged; no schema migrations; member removal from tenants by superadmin is not in scope.
