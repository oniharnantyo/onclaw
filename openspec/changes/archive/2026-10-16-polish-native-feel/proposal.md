# Proposal: polish-native-feel

## Why

The native-feel audit (2026-10-16, `docs/native-feel-audit.md`) found the
desktop app ships with placeholder branding (the default Wails "W" dock/tray
icon, template `Info.plist` strings like "My Product" that the About panel
shows), allows two instances to fight over one SQLite store, and leaks web
idioms (browser context menu on right-click, pointer cursors, dialogs that
ignore Escape). Each item is small; together they are the difference between
"a web page in a window" and a tray utility that reads as a regular Mac app.

## What Changes

- **Packaging identity:** real app icon replaces `build/appicon.png`
  (dock, tray template glyph, `icons.icns` regenerate from it);
  `build/darwin/Info.plist` gets honest name/version/copyright strings and
  the standard macOS keys it is missing.
- **Single instance:** launching a second copy focuses the existing window
  instead of starting a second process (two tray icons, two gateways, sqlite
  contention).
- **No browser context menu:** right-click does nothing (or nothing web) in
  the desktop window; Wails' `DefaultContextMenuDisabled` is enabled.
- **Window frame persistence:** size and position survive quit/relaunch.
- **Dialog keyboard discipline:** Escape closes every modal; each dialog
  focuses its primary input on open (all dialogs, not just agents').
- **Native input conventions:** no pointer cursor on controls, chrome
  (labels, menus, buttons) is not text-selectable while transcripts and
  inputs stay selectable, and `prefers-reduced-motion` collapses all app
  motion, not just card hover.
- **Verified, not assumed:** the square window corner (`CornerType:
  MacWindowCornerTypeSquare`) is confirmed as wanted for the Liquid Glass
  rim or reverted to platform rounding.

Deliberately NOT changed (locked design decisions, see audit "Deliberate
deviations"): self-hosted fonts, coral accent, theme-toggle-driven window
appearance, in-window settings, glass DOM modals, hover tints.

## Capabilities

### New Capabilities

- `ui-conventions`: input and motion conventions that make the WebView UI
  read as native — dialog keyboard behavior (Escape, autofocus), cursor and
  text-selection rules, and global reduced-motion compliance.

### Modified Capabilities

- `app-shell`: adds desktop-shell requirements — single-instance launch,
  truthful bundle identity (icon, tray glyph, About strings), suppressed
  default context menu, and window frame persistence. The existing deltas in
  flight (hide-on-close lifecycle, tray, application menu) are unchanged.

## Impact

- `build/appicon.png`, `build/darwin/icons.icns`, `build/darwin/Info.plist`
  (+ `Info.dev.plist`), tray icon setup in `main.go`.
- `main.go` window options (`DefaultContextMenuDisabled`, single-instance
  options, frame save/restore) — Go changes carry tests where the repo has
  them (`internal/cli`, service tests) and manual GUI verification for
  window chrome that only exists on macOS.
- `frontend/src/styles.css` (cursor/selection/reduced-motion layer),
  `frontend/src/components/ModalShell.tsx` + dialog components (Escape,
  autofocus), `frontend/src/features/settings/components/*ProvidersPane|
  HooksPane|McpPane*` (drop explicit pointer cursors).
- No API, store-schema, or bridge changes. Server mode is unaffected
  (headless: no menu, no tray, no window — new requirements are scoped to
  the desktop host).
