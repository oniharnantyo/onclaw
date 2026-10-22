# Design

## Context

The layout converged through viz previews (this session) against the real
design tokens; captured screenshots exist in the session's visualization
folder. Current code: `App.tsx` renders the desktop `h-9` spacer strip, the
`glass-chrome` rail (`App.tsx:214`), and routes Chat/Agents/Settings into one
`main.glass-surface` island. Chat already owns a `SessionRail`
(`features/chat/components/SessionRail.tsx` — agents + sessions inside the
chat surface, `glass-panel w-52`), and Settings renders via `SettingsView`
with its own pane sidebar (per `refactor-settings-providers`, 18/20 applied).
The 50px invisible titlebar (`main.go:131`) eats single clicks in the top
band (`webview_window_darwin.m` performs a window drag on first mouse-down),
which is why the strip must stay text-only.

## Goals / Non-Goals

**Goals:**

- Title-only strip driven by the active view (nav store), zero controls.
- Rail collapse (icon-only) with persisted preference.
- Sessions island: own column, header + new-session, search filter,
  title + meta entries, primary-tint active state.
- Settings as the full work-area island; sessions island hidden meanwhile.
- Pane header action row; provider rows with brand logo tiles.

**Non-Goals:**

- Any `main.go` / drag-band change — the strip holds no controls.
- The providers port, secrecy, or verification behavior
  (`refactor-settings-providers` owns those; this change adds presentation).
- The agents feature layout, RightSidebar, mobile (<lg) bar redesign.
- Global search (⌘K) — out of scope; the Sessions search filters titles only.

## Decisions

**D1 — Strip is part of `App.tsx`, driven by the nav store.** Replace the
`aria-hidden` spacer with a text row (height 40px, `ps` clearance past the
traffic lights) rendering `menu === "settings" ? "Settings" : "Chat"` plus a
per-view context line. Text-only means the native band is untouched; no
`-webkit-app-region` work, no `main.go` edit. The below-`lg` header island
keeps today's behavior.

**D2 — Rail collapse via a `useLocalPreference`-style hook (localStorage).**
Toggle lives in a new rail header row (panel-left icon). Collapsed width
~52px, labels hidden; the settings/theme cluster centers its icons. Persist
under a `buddies.railCollapsed` localStorage key — host UI state, not the
settings store (same reasoning as `~/.buddies/window.json`).

**D3 — Sessions island is the existing `SessionRail`, restyled and lifted.**
Keep its store wiring (agents, sessions, select/new); drop the in-panel
agents list (the shell rail owns navigation) — sessions only — restyle as
`glass-chrome island` column (matching the rail), add the title/meta rows and
a client-side title filter (`useState` in the component; no store change).
The meta line uses what the store already exposes (relative time; model when
present on the session). Hide via the same `data-active-menu`/nav-store
conditional that hides it for Settings.

**D4 — Settings fills the work area.** `SettingsView` already renders
sidebar + content; the shell change is to hide the Sessions island and any
chat chrome while `menu === "settings"` and let the settings island take the
full row. The strip title swaps per D1. No routing change — nav store only.

**D5 — Pane header action row as a small shared primitive.** A `PaneHeader`
component (title + optional `actions` slot, one row, both themes) used by the
settings content pane; Providers passes "Add provider". Avoids per-pane drift.

**D6 — Brand marks as a static map + fallback glyph.** `lib/brandMarks.tsx`
maps provider kind → inline SVG path (simple-icons glyphs; Anthropic in its
clay #D97757, OpenAI theme-following monochrome). Unknown kinds render an
initial-glyph tile. Inline SVG (not `<img>` CDN) keeps the offline desktop
requirement — the app must work without network.

## Risks / Trade-offs

- [Strip title overlaps traffic lights on narrow windows] → keep the 40px
  strip's text inset beyond the lights; below-`lg` keeps the existing header
  island.
- [Sessions island loses agent switching] → the shell rail's agent entry /
  Agents view remains the agent surface; watch for regression in "switch
  agent mid-chat" flows (manual check).
- [`user-select: none` chrome rules + search input] → inputs re-allow text
  selection already (native-conventions layer); verify the new search pill
  inherits the I-beam carve-out.
- [Brand tiles in light theme] → Anthropic clay and neutral tiles tested in
  both themes in the preview; OpenAI mark follows `--color-base-content`.
- [Two in-flight changes touch the same files] → `refactor-settings-providers`
  is 18/20 applied; land this after it finishes to avoid churn in
  `ProvidersPane`.

## Migration Plan

Pure frontend layout change; no store schema, API, or bridge changes.
Rollback is a plain revert. Land after `refactor-settings-providers` closes
its last tasks.

## Open Questions

- Rail-collapsed icon tooltips (title attr vs custom) — settle at apply time.
- Session meta when the store lacks a model field: relative time only
  (preview shows the model opportunistically).
