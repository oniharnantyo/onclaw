# Reshape Settings Page and Dialogs — Design

## Context

Workspace settings is `SettingsModal` — a wide modal with an internal eight-tab rail — opened from the Rail ⚙ button and the onboarding screen via `ui.settingsOpen`/`ui.settingsTab` in the ui store. Five panes (workspace, members, integrations, keys, notifications) are inlined in the modal component; `ProvidersPane`, `McpPane`, `SkillsPane`, and `KeyRow` sit beside it in `web/src/modals/`. Meanwhile the app already treats comparable surfaces as pages: admin routes hide the workspace sidebar and render their own full-area layout, and `AgentConfigModal`/`CronEditorModal` establish the structured-form dialog pattern launched from a page.

The design prototype rendered settings as a modal; this change deliberately evolves past that (the prototype manifest's own screen-file-first policy favors per-screen routes). Design tokens, typography, and interaction states carry over unchanged — only the container and form placement change.

## Goals / Non-Goals

**Goals:**
- URL-backed settings navigation: deep links, browser back/forward, refresh-survival
- Uniform list-plus-dialog CRUD across all settings entities, including new edit flows for MCP servers and skills
- One home for settings code (`web/src/screens/settings/`) and dialogs-only in `web/src/modals/`
- Preserve every existing pane behavior and test-coverage intent

**Non-Goals:**
- No backend, REST, or dependency changes — API keys, MCP servers, and skills remain workspace seed-data persistence
- Agent config modal, cron editor modal, and integrations behavior untouched
- No design-token or visual-language changes beyond the container swap

## Decisions

### 1. Route shape: `/settings/:section` with redirect
`/settings` redirects to `/settings/workspace`; the eight sections are path segments (`workspace`, `providers`, `members`, `integrations`, `mcp`, `skills`, `keys`, `notifications`); unknown sections redirect to the workspace section. Chosen over query-param (`?tab=`) or pure component state because path params are deep-linkable, match the existing `/admin/:screen` convention, and make the sidebar-hiding branch trivial (`pathname.startsWith('/settings')`). Alternative rejected: keeping `/admin`-style flat paths (`/settings-members`) — no precedent in the app.

### 2. Layout: settings owns its layout; sidebar hidden via the admin branch
The `Layout` component's view classification gains `settings` (`location.pathname.startsWith('/settings')`), and the sidebar-hiding condition extends to settings routes exactly as it already excludes admin. `SettingsPage` renders the section nav (today's modal tab-rail logic, promoted to page nav: static left column ≥768px, horizontal scroll tabs below) beside a `max-w` content column (matching `AdminView`'s centered content). The Rail stays visible. Alternatives rejected: keeping the workspace sidebar (three-column squeeze at 1024px), fullscreen takeover (breaks Rail consistency).

### 3. URL is the single source of truth for the active section
`ui.settingsOpen`/`ui.settingsTab` are deleted, not deprecated. Rail ⚙ and onboarding's "Workspace settings" navigate to `/settings`. Alternatives rejected: mirroring the section into the store (two sources of truth, sync bugs); compat aliases are against project rules. The `chat/runtime.test.ts` ui fixture and `App.test.tsx` update with the shape change.

### 4. Dialogs wrap existing form logic; panes own list state
Each pane owns a small dialog state (`{mode:'add'} | {mode:'edit', entity} | null`), passes the extracted form into a dialog component, and receives the entity back via `onSaved`. Dialogs reuse the `Modal` primitive verbatim — focus trap, Escape, aria-modal come free, and the enter-confirm-leave model fits. MCP edit and skill edit are new UI capability but purely local-state (seed-data store), so no backend concern rides along.

### 5. Sections extract verbatim, then dialogs are carved out
Five section components (workspace, members, integrations, keys, notifications) are extracted from `SettingsModal.tsx` without behavior change; `ProvidersPane`/`McpPane`/`SkillsPane`/`KeyRow` move with history preservation (git mv). Only after a section exists standalone does its dialog extraction happen — each step stays testable.

### 6. Tests split by surface
`SettingsModal.test.tsx` (~700 lines) splits into `SettingsPage.test.tsx` (nav, routing, sidebar-hidden, deep link), per-section tests (assertions ported, mounts re-wrapped), and new dialog tests. `App.test.tsx` and `chat/runtime.test.ts` update with the ui shape.

## Risks / Trade-offs

- [~700-line test migration] → Assertions port verbatim; only mount wrappers and form-opening steps change. Full vitest run after each migration step.
- [Leave-workspace flow loses its "close the modal" step] → After leaving, existing logic navigates to the next membership or /welcome; the settings page unmounts naturally.
- [Escape no longer closes settings] → Intentional page semantics; dialogs still Escape-close via the Modal primitive.
- [1920px content stretch] → `max-w` content column prevents; matches AdminView precedent.
- [Unarchived providers delta conflicts] → Resolved: `add-tenant-providers-management` was archived before the specs delta was authored; the validator confirms the MODIFIED Providers requirement applies cleanly.

## Migration Plan

Frontend-only; ships as one unit with no data migration. Rollback is a git revert. Internal ordering: extract sections → carve dialogs per section → route swap + store cleanup + entry-point rewiring → migrate tests → delete `SettingsModal.tsx` last (after nothing imports it).

## Open Questions

None. (When a server-backed API-keys domain lands, `ApiKeyDialog`'s submit target swaps without UI restructuring — a future change's concern.)
