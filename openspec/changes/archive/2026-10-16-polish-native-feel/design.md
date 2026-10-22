# Design

## Context

Wails v3.0.0-beta.29 on darwin (`-tags production,private_mac_apis`), window
created in `main.go:runDesktop` with hide-on-close, Liquid Glass backdrop,
tray + application menu. No main specs exist yet (`openspec list --specs` is
empty); both in-flight changes carry `app-shell` deltas, so this change's
`app-shell` delta adds requirements alongside theirs. The audit
(`docs/native-feel-audit.md`) holds the item-by-item evidence with file
references.

## Goals / Non-Goals

**Goals:**

- Desktop shell: single instance, honest bundle identity, no web context
  menu, persisted window frame.
- Frontend: Escape/focus discipline in every dialog, native cursor and
  selection scope, global reduced-motion compliance.
- Everything holds in both daisyUI themes and does not regress server mode
  (headless host — no window/tray/menu code paths).

**Non-Goals:**

- Locked design decisions (fonts, coral accent, theme-driven window
  appearance, in-window settings, glass DOM modals) — see proposal.
- URL schemes, notifications, auto-update, crash reporter, login items —
  future system-integration work, not in the audit's fix batch.
- Redesigning dialog visuals — keyboard/focus behavior only.

## Decisions

**D1 — Single instance via Wails `SingleInstanceOptions`, not plist.**
`application.Options.SingleInstance{UniqueID: "com.oniharnantyo.buddies",
OnSecondInstanceLaunch: …window.Show()}` in `main.go`. Alternative
`LSMultipleInstancesProhibited` in Info.plist is macOS-only, silently
bounces the second launch, and gives no focus callback; the Wails mechanism
is cross-platform (Windows matters later) and gives us the show-window hook.
The UniqueID is the bundle identifier so dev and packaged builds agree.

**D2 — Two icon assets, not one.** `build/appicon.png` becomes the real
full-color buddies icon (source for dock + `icons.icns` regeneration); the
tray gets a dedicated monochrome template PNG (black + alpha) passed to
`SetTemplateIcon`. Reusing the square app icon as a template renders a solid
blob — template icons are alpha masks. Icon artwork is asset work, tracked
as a task, not code.

**D3 — Info.plist filled per Apple conventions; no instance prohibition
key.** `CFBundleName: buddies`, real copyright/get-info strings, add
`LSApplicationCategoryType` (`public.app-category.utilities`) and
`NSPrincipalClass` (`NSApplication`); `CFBundleIdentifier` is already
correct. `LSMultipleInstancesProhibited` is deliberately omitted (D1).
Validate with `plutil -lint`; mirror user-facing strings into
`Info.dev.plist` only where they affect dev-run behavior.

**D4 — `DefaultContextMenuDisabled: true` unconditionally on the desktop
window.** One line in `WebviewWindowOptions`. Dev debugging keeps DevTools
via the dev build path; the web/server frontend is untouched (browsers
supply their own menu). Alternative — JS `contextmenu` preventDefault —
fights the platform instead of using the Wails switch.

**D5 — Window frame persisted to `~/.buddies/window.json`, not SQLite.**
`main.go` saves x/y/width/height on window move/resize (Wails window
events), restores before first show, and clamps the saved frame against the
current displays (Wails screen enumeration) so an unplugged monitor cannot
strand the window. A JSON file keeps host UI state out of the app settings
schema (no store migration); the file is best-effort — a missing/corrupt
file means default frame, never a crash.

**D6 — Escape + autofocus at `ModalShell`, plus per-dialog `autoFocus`.**
`ModalShell` gains a `useEffect` keydown listener (Escape → `onClose`);
dialogs missing an initial focus get `autoFocus` on their primary input
(`ProviderFormDialog` is the gap; agents' dialogs already have it). One
modal is open at a time in this app, so no topmost-dialog stack is needed.
Vitest: ModalShell Escape case + a focus case in `renderUI`.

**D7 — One conventions layer in `styles.css`, not per-component classes.**
An unlayered CSS block: cursor rules (`button, [role="button"], .menu li >
*, label { cursor: default }`, inputs keep I-beam), selection scope
(`user-select: none` on chrome selectors; transcripts/inputs untouched),
and a global `prefers-reduced-motion` collapse (all transitions/animations
to opacity-only ≤150ms, focus-ring transitions removed). The three explicit
`cursor-pointer` classes in `ProvidersPane`/`HooksPane`/`McpPane` are
deleted. Component-level overrides would re-introduce the drift this
change exists to kill.

**D8 — Corner type: verify, then keep or revert.** `CornerType:
MacWindowCornerTypeSquare` stays unless the macOS check shows platform
rounding works with the glass rim; the check is a task, the decision is a
one-line revert if it fails.

## Risks / Trade-offs

- [Single-instance stale lock after a crash] → Wails owns the socket
  lifecycle; verify relaunch after `kill -9` in the manual pass.
- [Tray template glyph unreadable on light menu bars] → template PNG is
  pure black + alpha; verify on both menu bar appearances.
- [Plist edits break the build] → `plutil -lint` gate + `task build` before
  finishing any Go/asset change.
- [`user-select: none` too broad (users copy model names)] → selectors are
  chrome-only; transcripts, inputs, and `.font-mono` model lines stay
  selectable — verify copy-from-model-line in the manual pass.
- [Global reduced-motion rule hides intentional feedback] → rule collapses
  to ≤150ms opacity, never removes state changes; hover tint (a color
  change) survives.
- [Window JSON grows stale across displays] → clamp-on-restore + corrupt
  file fallback to default frame.

## Migration Plan

Asset + code changes only; no store schema, API, or bridge changes. Rollback
is a plain revert. Icon/plist changes land together so the packaged bundle
is never half-branded.

## Open Questions

- Final icon artwork (D2): the user supplies a mark, or we draft a simple
  wordmark glyph as the starting point — does not change the approach.
- D8 corner-type verdict requires the unlocked Mac; defaults to keep.
