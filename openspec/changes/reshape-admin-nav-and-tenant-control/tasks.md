## 1. Backend — tenant control plane

- [x] 1.1 Add `PATCH /admin/workspaces/:id` (rename + timezone, creation-grade validation, master-rename → 400) in `admin_workspaces.go` with handler tests
- [x] 1.2 Add `PATCH /admin/workspaces/:id/owner` — atomic swap tx: target → built-in Owner role, other owners → built-in Admin role, auto-add non-member target; master → 400; handler tests for all four spec scenarios
- [x] 1.3 Add `GET/POST /admin/workspaces/:id/members` — list any tenant's members; add existing user with Admin/Member only (Owner → 400, already-member → 409, unknown user → 404); handler tests
- [x] 1.4 Register routes in `router.go` under the admin group with existing admin guards; wire into store `Members` port where listing reuses `ListForWorkspace`
- [x] 1.5 Add `is_superadmin` to `GET /admin/users` (master-tenant membership ∧ role.IsOwner) with a test covering flag true/false/after-promote
- [x] 1.6 Master-tenant modification rule: superadmins MAY rename/re-zone master (admin PATCH + settings PATCH); suspension and owner transfer of master stay refused. Guards lifted in `admin_workspaces.go` + `workspaces.go`, Go tests flipped/added, spec deltas updated

## 2. Web — shared primitives

- [x] 2.1 Build `Tooltip` primitive: hover + focus-visible, 150ms delay, Escape dismiss, right placement, `role="tooltip"` + `aria-describedby`, token-styled; unit tests
- [x] 2.2 Build `Combobox` (searchable dropdown: filter by substring, keyboard nav arrows/enter/escape, token-styled) and `TimezoneSelect` specialization (`Intl.supportedValuesOf('timeZone')` with UTC offset per entry, static fallback list); unit tests
- [x] 2.3 Build `UserPicker` specialization (fed by `GET /admin/users`, shows name + email + disabled state); unit tests
- [x] 2.4 Wire `TimezoneSelect` into `CreateTenantModal` (replace 6-zone native select) and `SettingsModal` workspace pane (replace 5-zone native select)

## 3. Web — nav reshape

- [x] 3.1 Routes: `/admin/workspaces` + `/admin/accounts` (+ redirect from `/admin`, `/admin/:tab`); update `view` derivation and rail active-item mapping
- [x] 3.2 Rail: replace the single Admin item with Workspaces (globe) + Accounts (users), both `showAdmin`-gated; remove the sidebar's dead "Instance Admin" button
- [x] 3.3 Restrict the onboarding bouncer to chat routes (`/`, `/c/*`); `/agents`, `/cron`, `/runs` render empty states in zero-agent workspaces
- [x] 3.4 `/welcome` highlights nothing in the rail (no `chats` fallback highlight)

## 4. Web — rail tooltips + expand/collapse

- [x] 4.1 Rail renders labels when expanded (width transition on motion tokens); same `items` array feeds icon/label/tooltip; unread badge relocates inline
- [x] 4.2 Expand/collapse toggle at rail bottom (hidden <768px); state persisted via the `pos` localStorage key; tooltips replace `title` attrs on all icon-only rail controls (switcher, nav items, settings, menu)
- [x] 4.3 Update rail/sidebar vitest coverage; keep default-collapsed render stable for the visual-parity suite

## 5. Web — tenants screen

- [x] 5.1 `CreateTenantModal`: owner email field → `UserPicker` (existing users only); keep per-field errors + toasts
- [x] 5.2 Build `EditTenantModal`: rename + timezone save, owner transfer section (shows current owner(s), pick new owner, confirm), add-member section (user picker + Admin/Member role select, list with role badges)
- [x] 5.3 `TenantsPane` rows gain an Edit action opening `EditTenantModal`; toasts surface demotions and guard rejections
- [x] 5.4 Update `AdminView` split: Workspaces pane + Accounts pane as separate screens; update `AdminView.test.tsx`

## 6. Web — accounts/users screen

- [x] 6.1 `UsersPane`: add superadmin badge column, inline promote/demote actions (response-driven updates, guard toast on `last_owner_protected`)
- [x] 6.2 Delete `SuperadminsPane` and absorb promote/demote coverage into `UsersPane` tests
- [x] 6.3 Remove plan: `types.ts` field, `seed.ts` + `blankTenant` param, Sidebar/OnboardingPane/SettingsModal/WorkspaceSwitcher render sites, dead Free/Pro picker already gone with the modal
- [x] 6.4 Remove the switcher's "Create workspace" entry, `CreateWorkspaceModal` + test, `createWsOpen`/`createWorkspace` store pieces
- [x] 6.5 Update `App.test.tsx` / nav tests for two gated rail items and no-creation switcher

## 7. Verification

- [x] 7.1 `go build ./... && go vet ./... && go test ./...` green; integration tests with `TEST_DATABASE_URL` for the new endpoints
- [x] 7.2 `./scripts/smoke.sh` extended with tenant edit / owner transfer / tenant members flows
- [x] 7.3 `pnpm test` green (vitest); `pnpm build` typecheck green
- [x] 7.4 `pnpm test:e2e` visual parity unaffected (default collapsed rail)
- [x] 7.5 Update `web/README.md` / CLAUDE.md command docs if flows changed
