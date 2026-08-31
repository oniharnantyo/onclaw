# Reshape Settings Page and Dialogs

## Why

Workspace settings is a wide modal with an internal tab rail, so it has no URL, no browser back/forward, and no deep links — and every CRUD form inside it is inline, including a "Create key" button that mints a key with no form at all. Meanwhile the app already treats comparable surfaces as pages: admin screens are routed views with the sidebar hidden, and the design contract's screen-file-first policy says each distinct surface is its own route. MCP servers and skills additionally have no edit flow anywhere.

## What Changes

- **Settings becomes a routed page** — `/settings` redirects to `/settings/workspace`; `/settings/:section` renders the section (workspace, providers, members, integrations, mcp, skills, keys, notifications). The URL replaces the `ui.settingsOpen`/`ui.settingsTab` store fields (both removed). The workspace sidebar is not rendered on settings routes; the settings surface presents its own section nav (static left column ≥768px, horizontal scroll tabs below). `SettingsModal` is deleted.
- **Five modal-inline panes become section components** under `web/src/screens/settings/` — workspace, members, integrations, keys, notifications — alongside the relocated `ProvidersPane`, `McpPane`, `SkillsPane`, and `KeyRow` (which move out of `web/src/modals/`).
- **All add/update flows become dialogs** (`web/src/modals/` keeps dialogs only):
  - `ProviderFormDialog` — add + edit (write-only key; "leave blank to keep" on edit preserved)
  - `McpServerDialog` — add + **new edit flow** for name/transport
  - `SkillDialog` — install + **new edit flow** for name/description
  - `ApiKeyDialog` — create with a name field (fixes the formless mint) and the one-time full-key reveal
  - `InviteMemberDialog` — email + role from the workspace's real roles
- **Store cleanup** — `settingsOpen`/`settingsTab` removed from the ui store; Rail and onboarding settings buttons navigate to `/settings`.
- Out of scope: backend changes, agent config modal, cron editor modal, real integrations API, server-backed API keys and MCP/skills persistence (still workspace seed-data), key rotation.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `web-app/settings`: Settings navigation changes from a tabbed modal to a routed page with its own section nav (URL carries the section). Members invite, MCP add, skill install, and API key create move into dialogs; MCP servers and skills gain edit flows. The providers pane's add/edit moves into a dialog.
- `web-app/shell`: Screen routing gains the `/settings` → `/settings/:section` routes; the route list in the Screen routing requirement is updated.

## Impact

- **Frontend code**: `web/src/App.tsx` (routes, layout branch, entry-point wiring), `web/src/store/index.ts` (ui shape), `web/src/components/nav/Rail.tsx`, `web/src/screens/OnboardingPane.tsx`, deletion of `web/src/modals/SettingsModal.tsx`, new `web/src/screens/settings/` section components, new dialog components in `web/src/modals/`.
- **Tests**: `SettingsModal.test.tsx` (~700 lines) splits into `SettingsPage.test.tsx`, section tests, and dialog tests; `App.test.tsx` and `chat/runtime.test.ts` update for the ui store shape.
- **Spec sequencing**: `add-tenant-providers-management` (complete, unarchived) carries the current Providers-pane requirement in its unsynced delta — archive it **before** this change so the MODIFIED providers requirement applies cleanly.
- **No backend/API/dependency changes.**
