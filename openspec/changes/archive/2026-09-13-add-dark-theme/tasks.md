## 1. Theme engine and no-flash boot

- [x] 1.1 Create `web/src/lib/theme.ts`: pure resolver (stored pref `null | 'light' | 'dark'` + OS scheme → resolved theme), controller that sets `data-theme` on `document.documentElement`, persists to a namespaced local-storage key, and subscribes to `prefers-color-scheme` changes only while preference is system
- [x] 1.2 Add the inline bootstrap script to `web/index.html` before the module tag (read key → resolve via `matchMedia` → set attribute) with a cross-referencing comment tying it to `theme.ts`; pin key name and resolution rule with a `theme.test.ts` unit test
- [x] 1.3 Unit tests for the resolver and cycle order: fresh browser → system; stored overrides OS; system tracks live scheme changes; cycle light → dark → system → light

## 2. Dark token palette

- [x] 2.1 Add the `:root[data-theme="dark"]` override block to `web/src/styles/tokens.css` with the pinned palette (bg `#111111`, surface `#1a1a1a`, warm `#222222`, fg `#ededed`, fg-2 `#c9c9c9`, muted `#9a9a9a`, border `#2c2c2c`, border-soft `#232323`; accent, accent-on, and status hues unchanged), activating the alias tiers as real values; leave the light `:root` block byte-identical
- [x] 2.2 Verify derived tokens adapt in dark without edits (`--accent-hover`/`--accent-active`, `--elev-raised`, focus ring, scrollbar, `::selection`) — eyes-on only, fix only if broken

## 3. Cycle control

- [x] 3.1 Add `sun`, `moon`, `monitor` icons to the `Icon.tsx` catalog in the existing stroke style
- [x] 3.2 Create the shared theme cycle button (component or composable used by both homes): icon mirrors current mode, click cycles light → dark → system → light, tooltip contract "Theme: <current> (click for <next>)", accessible name reflects state
- [x] 3.3 Mount in `Rail.tsx` bottom cluster directly above Settings: expanded = icon + "Theme: <mode>" label row; collapsed = 44px icon button with right tooltip; confirm the mobile drawer inherits it
- [x] 3.4 Mount icon-only variant in `LoginView.tsx`, absolutely positioned top-right of the viewport with a bottom tooltip
- [x] 3.5 Component tests: cycle order from each state, icon-per-state mapping, tooltip/aria-label text, rail renders the control in both collapsed and expanded shapes, login renders it

## 4. Semantic sweep

- [x] 4.1 Replace the ~11 `text-white` accent/danger button labels with `text-accenton` (settings panes, `ToolCall.tsx`, `AgentCard.tsx`, `admin/UsersPane.tsx`); confirm zero visual diff in light via the screenshot suite

## 5. Verification

- [x] 5.1 Run the touched vitest suites and the web build; both green
- [x] 5.2 Playwright dual-theme screenshot sweep across chat, agents, schedules, runs, settings sections, admin, and login; review dark renders and tune palette deltas as token-file edits only
- [x] 5.3 Manual dark pass on the detail surfaces: markdown code blocks, tool-call cards, context meter, mention/slash pills, streaming states, LoginView and BootError pre-auth (bootstrap correctness), AvatarPicker, UsersPane color-mix hover
- [x] 5.4 Manual light-theme regression pass: light renders byte-identical to pre-change (tokens untouched, sweep is no-op visually)
