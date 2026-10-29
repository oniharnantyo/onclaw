# Proposal

## Why

The desktop nav island (Chat / Agents / Schedules / Knowledge + the Theme/Settings cluster) renders its own row grammar — tall `h-11` icon cards with a 12px radius and an accent-text active state — while the sessions rail beside it already speaks the flatter menu grammar users know from git-helper's left menu (compact rows, tinted selection pill, muted utility strip). The nav island is the last chrome surface that reads as a different design system, and the user wants its look replaced with git-helper's menu style.

## What Changes

- Restyle the expanded nav island's menu rows to the flat rail-row grammar the sessions rail already uses (`SideRow`: `h-[30px]`, `rounded-md`, full-width, truncated label) — git-helper's menu look, adapted to the existing design kit.
- Keep small icons (~16px) at the left of each nav row (user decision; git-helper is text-only, but the icons aid scanning and survive the collapsed rail unchanged).
- Active row state becomes the SideRow grammar: accent-tint pill with plain foreground text (no accent-colored text, no forced semibold) — closer to git-helper's selected row.
- No group headings: the menu stays one flat list (user decision); the bottom Theme/Settings cluster keeps its `border-t` separator as the only division.
- Align the bottom cluster to the same row height: `ThemeCycleButton`'s rail-expanded branch drops from `h-11` to the 30px row height, keeping its state-bearing icon; the Settings row keeps its `rail-settings` testid.
- Collapsed 80px rail and the below-lg mobile top bar are unchanged.
- Remove the island's own card (user decision, 2026-10-28: "just remove the island border"): the desktop rail renders as a flat column on the window canvas — no `glass-chrome` surface, hairline border, or rounded corners — so the left edge reads like git-helper's sidebar flush against the window, while the content island stays the carded surface. Rows, bottom cluster, collapse behavior, and rail widths are unchanged.
- Pin the resulting grammar as an ADDED requirement on `app-shell` ("Frontend navigation menu") so future shell changes keep it (user decision).

## Capabilities

### New Capabilities

<!-- none — no new capability introduced -->

### Modified Capabilities

- `app-shell`: ADDED requirement "Frontend navigation menu" describing the desktop nav island's observable grammar (flat compact rows with leading icons, tinted selection pill, collapsed icon-only mode, mobile bar unchanged), so the visual contract is spec-pinned rather than code-only.

## Impact

- `frontend/src/App.tsx` — `NavItems` rail branch and `RAIL_ROW_*` constants; `RailSettingsItem` row geometry; the rail `aside` loses its `glass-chrome`/border/surface card and gains a `nav-rail` testid.
- `frontend/src/components/ui/SideRow.tsx` — optional `testId` prop (backward-compatible) so nav rows can carry `rail-settings`.
- `frontend/src/components/ui/ThemeCycleButton.tsx` — rail-expanded branch height/geometry only; icon variant and label/aria contract untouched.
- `frontend/src/__test__/App.test.tsx` — new assertions for the flat-row grammar and the card-less rail column (`railAside()` locates the rail by `data-testid="nav-rail"` instead of the removed `aside.glass-chrome` class); existing contract (labels present expanded, icon-only collapsed with aria-labels, `rail-theme` label/aria cycle, `rail-settings` testid) must keep passing.
- No backend, bindings, or window/tray lifecycle changes; desktop and server (web) hosts render the same frontend, so both hosts get the new menu look.
