## 1. Extract settings sections (behavior-neutral)

- [x] 1.1 Create `web/src/screens/settings/` and extract the five inlined panes from `SettingsModal.tsx` into section components — `WorkspaceSection` (incl. danger zone), `MembersSection`, `IntegrationsSection`, `KeysSection`, `NotificationsSection` — passing `tenant`/`onToast`/`onUpdate`/navigation callbacks as props; `SettingsModal` renders them from their new home so all existing tests still pass
- [x] 1.2 Move `ProvidersPane`, `McpPane`, `SkillsPane`, `KeyRow` from `web/src/modals/` to `web/src/screens/settings/` with history preservation and update imports; full vitest run stays green

## 2. Carve dialogs per section

- [x] 2.1 `ProviderFormDialog` (add + edit) in `web/src/modals/`; `ProvidersPane` opens it from "Add provider" and per-row edit, dropping the inline add form and inline edit cards; port add/edit/delete/toggle/verify tests to the dialog flow
- [x] 2.2 `McpServerDialog` (add + new edit) — `McpPane` gains a per-row edit control opening the dialog for name/transport; add tests for add-via-dialog and edit
- [x] 2.3 `SkillDialog` (install + new edit) — `SkillsPane` gains a per-row edit control for name/description; add tests for install-via-dialog and edit
- [x] 2.4 `ApiKeyDialog` — `KeysSection` "Create key" opens a dialog with a required name field and a one-time full-key reveal with the copy warning; add create and name-required tests
- [x] 2.5 `InviteMemberDialog` — `MembersSection` invite row becomes a button opening a dialog (email + role from real roles); port invite-validation test, add dialog-submit test

## 3. Route swap, layout, and store cleanup

- [x] 3.1 Add `/settings` (redirect to `/settings/workspace`) and `/settings/:section` routes in `App.tsx` with unknown-section redirect; extend the view classification with `settings` and hide the workspace sidebar on settings routes (admin branch precedent)
- [x] 3.2 Build `SettingsPage` in `web/src/screens/settings/` — section nav (static left column ≥768px, horizontal scroll tabs below) beside a max-width content column; sections render per URL param
- [x] 3.3 Rewire entry points: Rail ⚙ and onboarding's "Workspace settings" navigate to `/settings` instead of `patchUi({ settingsOpen: true, … })`
- [x] 3.4 Delete `settingsOpen`/`settingsTab` from the ui store shape and initial state; update `chat/runtime.test.ts` fixture and any `App.test.tsx` references

## 4. Test migration and final cleanup

- [x] 4.1 Port remaining `SettingsModal.test.tsx` coverage into `SettingsPage.test.tsx` (nav/routing/sidebar-hidden/deep link) and per-section tests; delete the old test file
- [x] 4.2 Delete `SettingsModal.tsx` once nothing imports it; grep for lingering `settingsOpen`/`settingsTab`/`SettingsModal` references
- [x] 4.3 Verify: full `pnpm test` green, `pnpm build` (typecheck) clean, and responsive smoke at 360px (horizontal-scroll section nav) and 1920px (no content stretch) with no horizontal overflow
