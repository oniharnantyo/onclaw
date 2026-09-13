# Add Dark Theme

## Why

The app renders light-only. Users who prefer dark interfaces (or whose OS is set to dark) meet a bright workspace at every session, including the very first screen they ever see — the login page, where no in-app control exists to fix it. The frontend is fully tokenized (every component resolves colors through `web/src/styles/tokens.css` via Tailwind v4 `@theme` mapping), so a dark theme is a token-tier extension today at near-zero component cost; the token file already carries dormant alias tiers (`--surface-warm`, `--fg-2`, `--border-soft`) reserved for exactly this.

## What Changes

- Add a **dark theme** as a `data-theme="dark"` override block in `tokens.css`. Light remains the frozen design-contract baseline; the dark palette is a derived extension of the same tokens (neutrals inverted: bg `#111111`, surface `#1a1a1a`, warm `#222222`, fg `#ededed`, muted `#9a9a9a`, border `#2c2c2c`; accent and status hues unchanged). Not Tailwind's `dark:` class variant — zero component-level color authoring.
- Add a **theme preference engine**: three-state `light | dark | system`, persisted to browser local storage (device-local — no backend, schema, or API changes), defaulting to `system` with live tracking of `prefers-color-scheme` changes, applied via a `data-theme` attribute on `<html>`.
- Add a **no-flash bootstrap**: a small inline script in `index.html` that applies the stored preference before the app module loads, so the login screen and every reload boot in the right theme.
- Add a **theme cycle control** in two homes (same component, same contract):
  - the nav rail's bottom cluster, directly above the Settings row — icon+label when expanded, icon-only with tooltip when collapsed;
  - the login screen as an icon-only button in the viewport's top-right corner.
  Clicking cycles `light → dark → system → light`; the icon mirrors the current mode (sun / moon / monitor); the tooltip announces current and next state.
- Sweep the ~11 `text-white` button labels (accent/danger backgrounds) to the semantic `text-accenton`, plus add the `sun`, `moon`, `monitor` icons to the icon catalog.
- Explicitly out of scope: any Settings page section (the user chose sidebar-only access), and a theme button on the BootError screen (it inherits the stored theme, but offers no control).

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `web-app/shell`: the frozen design-tokens requirement becomes theme-dependent (light baseline unchanged; dark derived palette added under `data-theme="dark"`); new requirements for the theme preference (three-state, device-local persistence, system tracking, no-flash boot) and the cycle control on the rail and login screen.

## Impact

- **Code (frontend-only):** `web/src/styles/tokens.css` (dark override block), `web/index.html` (bootstrap script), new `web/src/lib/theme.ts` (preference engine), `web/src/components/nav/Rail.tsx`, `web/src/screens/LoginView.tsx`, `web/src/components/ui/Icon.tsx` (3 new icons), and the `text-white` → `text-accenton` sweep across settings panes, `ToolCall.tsx`, `AgentCard.tsx`, `admin/UsersPane.tsx`.
- **No backend changes:** no Go code, no schema migrations, no API surface. Theme is a per-device browser preference in local storage, invisible to the server.
- **Verification:** unit tests for preference resolution; dual-theme Playwright screenshot pass; manual visual pass over every screen in dark (chat, tools cards, context meter, mention pills, markdown code blocks, schedules, runs, settings, admin, login, boot error).
