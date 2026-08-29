## Context

The admin area is one `AdminView` at `/admin/:tab` (Tenants / Users / Superadmins), gated by `useIsAdmin()` (master-tenant membership + `admin.*`). The members API (add/patch/delete) is workspace-scoped only — the actor's own workspace via `MustCurrentWorkspace` — so a superadmin cannot manage an arbitrary tenant's members today. Admin endpoints derive authority from `admin.*` permissions; member endpoints from workspace-role algebra (`canAssign`/`canEdit`) plus a last-owner guard. The web carries prototype-era `plan` fields and 5-zone timezone selects; the rail is icon-only with native `title` tooltips.

## Goals / Non-Goals

**Goals:**
- Split the admin area into Workspaces (tenants-only) and Accounts (users, absorbing superadmins).
- Full tenant editing: rename, timezone, owner transfer (exactly one owner), member add/list.
- One creation path (Tenants screen) and a shared searchable-combobox primitive.
- Fix nav active-state bugs and make the icon rail usable via tooltips + persisted expand/collapse.

**Non-Goals:**
- No schema migrations (ownership is a role on members; swap updates role assignments).
- Superadmin **member removal** from tenants — not in scope (add/list/transfer only).
- The backend self-service `POST /workspaces` endpoint stays callable; web stops using it.
- API-level auto-create-on-unknown-email is unchanged; the web owner picker lists existing users only.

## Decisions

1. **Routes `/admin/workspaces` + `/admin/accounts`** (keep the `/admin` prefix; shared gating; `/admin` and `/admin/:tab` redirect to `/admin/workspaces`). Alternatives considered: top-level `/workspaces`/`/accounts` (new top-level namespace, more redirect handling) and keeping `/admin` as the tenants screen (ambiguous with accounts). The rail items link there; `view` derivation in `Layout` treats any `/admin` path as the admin surface, and the rail highlights Workspaces vs Accounts per exact screen.

2. **Admin member/owner endpoints derive authority from `admin.workspaces.write` alone.** Workspace-role algebra does not apply — the actor has no role in the target workspace. This keeps "no cross-tenant powers by default" true: tenant routes still enforce membership identically; `/admin/*` is the sanctioned control-plane exception. Alternative: force superadmins to join each tenant to use member endpoints (rejected — control-plane flow with pointless ceremony).

3. **Owner transfer is an atomic swap in one transaction**: target user → the workspace's built-in Owner role; every other owner-role holder → built-in Admin role; non-member targets are added in the same tx. "Exactly one owner" is maintained operationally by the swap, not by a new domain invariant. Alternative: enforce exactly-one-owner as a global domain rule (rejected — a larger domain change that would also reject current multi-owner tenants).

4. **`is_superadmin` computed server-side** in `GET /admin/users` (membership in master tenant ∧ role.IsOwner). Client-side join with `/admin/superadmins` rejected: two calls, stale badges after promote/demote, join logic in the UI.

5. **One combobox primitive, three specializations**: shared searchable dropdown (hand-rolled, token-styled, keyboard nav — no new deps) powering `TimezoneSelect` (data from `Intl.supportedValuesOf('timeZone')` + UTC offset per entry, static fallback list for older browsers) and the owner/member `UserPicker` (fed by `GET /admin/users`).

6. **`Tooltip` primitive replaces `title` attributes on rail controls** — hover + `focus-visible` triggers, ~150ms show delay, Escape dismiss, right placement, `role="tooltip"` + `aria-describedby`. Native `title` removed where the real tooltip renders (double-tooltip otherwise). Default state **collapsed** so the default render stays pixel-identical for the Playwright visual-parity suite; expansion persists via the `pos` localStorage key alongside tenant/chat.

7. **Store cleanup rides along**: `createWsOpen` UI state, `CreateWorkspaceModal`, and the `createWorkspace` store action die with the self-service path; `blankTenant` loses its `plan` param.

8. **Rail item labels**: the two gated rail items are **Workspaces** and **Accounts**; the same `items` array drives icon, label, tooltip, and expanded label.

9. **Master tenant is superadmin-modifiable, not untouchable**: rename/timezone edits to the master tenant are allowed on the admin PATCH route (superadmin-gated) and on the settings route, where the rule is enforced transitively — the master tenant has no role with `workspace.write` other than Superadmin. Suspension and ownership transfer of master remain refused (suspending would lock the control plane; master ownership is governed by the superadmin promote/demote flow with its last-superadmin guard). This supersedes the earlier "master cannot be renamed" reading of the master-protection rule.

## Risks / Trade-offs

- [Owner swap silently demotes peer owners] → the edit modal lists current owners before transfer; success toast names demoted owners.
- [`is_superadmin` staleness after promote/demote] → update from the grant/revoke API responses, not refetch.
- [`Intl.supportedValuesOf` unsupported (Safari < 15.4)] → static fallback list of common zones; searchable the same way.
- [Rail expansion breaks visual-parity e2e] → collapsed by default; e2e runs against default state.
- [Admin member add bypasses canAssign] → intended by design; guard is `admin.workspaces.write` + role restricted to Admin/Member; documented in specs and tests.
- [Zero-membership users lose self-service creation] → accepted product decision (admin-provisioned); surfaced in proposal non-goals.

## Migration Plan

Backend first (additive: 3 endpoints + `is_superadmin` flag), web cutover second, dead-path deletion last. No DB migrations; rollback is reverting the web deploy — the additive endpoints are unused by the prior UI.

## Open Questions

None — owner-transfer semantics, edit scope, and change structure were resolved with the user during exploration.
