# Tasks

## 1. Shared components

- [x] 1.1 `SideRow` grows additive props: optional `testId` (renders `data-testid`) and optional `ariaCurrent` (renders `aria-current="page"` when true); default rendering and existing usage untouched
- [x] 1.2 `ThemeCycleButton` rail-expanded branch only: `h-11` → `h-[30px]`, `gap-3` → `gap-2`; icon size, `Theme: <pref>` label/aria contract, and the collapsed/icon variants stay as-is

## 2. Shell nav island (App.tsx)

- [x] 2.1 `NavItems` expanded rail branch renders `SideRow` rows (16px leading icons, truncated labels, `aria-current` on the active row); the collapsed branch keeps the current icon-only buttons and the below-lg bar branch is untouched
- [x] 2.2 `RailSettingsItem` expanded branch renders `SideRow` with `testId="rail-settings"`; collapsed branch unchanged; retire the now-unused `RAIL_ROW_EXPANDED` constant
- [x] 2.3 De-card the rail `aside` (design D7): drop `glass-chrome`/`border-line`/`bg-surface`/`rounded-lg` so the island is a flat canvas column; add `data-testid="nav-rail"` and re-point `App.test.tsx`'s `railAside()` at it

## 3. Tests

- [x] 3.1 `App.test.tsx`: flat-grammar assertions — compact structural rows in the rail nav, pill-carries-active-state (no accent text/semibold on the active row), bottom cluster at the same row height, settings row click-through, collapsed rail still operable via accessible names
- [x] 3.2 Full frontend vitest suite, `vp check src`, and `tsc` all green

## 4. Verification

- [x] 4.1 Verifier PASS with fresh evidence against the ADDED `app-shell` "Frontend navigation menu" scenarios
- [x] 4.2 Live smoke via server-mode build + CDP screenshots (expanded/collapsed × light/dark, evidence.pdf in .team/smoke/replace-menu-island-git-helper/)
