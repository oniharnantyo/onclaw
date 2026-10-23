# Tasks

## 1. Foundation (Wave 0)

- [x] 1.1 Port `styles/tokens.css` and `styles/theme.css` from onclaw-v2 into `frontend/src/styles/` with the documented import order (raw tokens after `@import "tailwindcss"`); keep the daisyUI blocks active alongside. Verify: `npm run build` passes and the built CSS contains both old and new token sets
- [x] 1.2 Add `clsx` + `tailwind-merge`, create `lib/cn.ts`; add the onclaw-v2 base rules (focus-visible contract, selection, `od-*` motion primitives, scrollbar) to `styles.css` namespaced so they don't fight daisyUI classes. Verify: pilot renders with the focus ring and `od-pop` utility in dev
- [x] 1.3 Port `lib/theme.ts` (three-state preference, resolve/cycle, OS-following) with its index.html inline no-flash bootstrap and the index.html assertion test; wire `stores/theme.ts` consumers to it unchanged in behavior. Verify: `theme.test.ts` passes; cold reload in both system appearances shows no wrong-theme flash
- [x] 1.4 Pilot migration: port `StatusDot` and build kit `Button` (variants: primary, ghost, destructive; sizes sm/md; 8 states per hallmark component discipline), migrate one screen region (composer send cluster) off `btn*` classes. Verify: `vp check` scoped to `frontend/src`, vitest, `npm run build`, both themes legible in dev, `task run` desktop spot check
- [x] 1.5 Create the standing daisyUI-usage grep script (denylist of the inventoried class families over `frontend/src`) and record the baseline count. Verify: running it prints a count that shrinks each wave
- [x] 1.6 Lift the brand-mark fill (raw `#D97757` in `lib/brandMarks.tsx`) into a token or `var()` reference. Verify: no raw hex in `frontend/src` outside tests

## 2. Shell (Wave 1)

- [x] 2.1 Port kit components the shell needs (MicroLabel, SectionLabel, Chip, Segmented, SideRow, Tooltip, Toasts; Base UI only where onclaw-v2 uses it) with their tests. Verify: ported components' vitest suites pass in `frontend`
- [x] 2.2 Migrate topbar, nav rail, and sessions rail off `menu`/`menu-active`/`badge*` classes onto kit + tokens; replace island/desk/card-lift grammar with onclaw-v2 surface/ring grammar in the shell. Verify: shell renders in both themes in dev; grep count drops accordingly
- [x] 2.3 Re-base liquid-glass tints (`--glass-*`, `html[data-glass]` rules) on the neutral tokens; keep opaque fallbacks as the real design. Verify: desktop `task build` + manual macOS check in light and dark system appearance, reduced-transparency opaque
- [x] 2.4 Shell-wave tests: update shell component tests that asserted daisyUI classes to assert roles/testids; run full frontend gates. Verify: `vp check` scoped to `frontend/src`, `vitest`, `npm run build` all green
- [x] 2.5 Make the rail theme-toggle read as a control, not a status: visible label agrees with its aria-label (pick current-theme or target-theme, not one each). Verify: label and aria-label consistent in both themes

## 3. Chat (Wave 2)

- [x] 3.1 Port Modal onto ModalShell/ConfirmDialog internals (state-driven visibility, Escape/focus per ui-conventions) and migrate all modal call sites off `modal*` classes. Verify: vitest suites for dialogs pass; Escape closes each dialog in dev
- [x] 3.2 Migrate transcript, composer, session rail, and tool cards off daisyUI classes, porting onclaw-v2's assistant-ui styling patterns (shadcn token seam where assistant-ui emits them). Verify: chat renders in both themes; grep count drops
- [x] 3.3 Port kit Alert/Skeleton/Input/Textarea/Select primitives in the onclaw-v2 idiom (8 states each) and migrate form call sites in chat surfaces. Verify: vitest for form components; both themes legible in dev
- [x] 3.4 Composer must never silently no-op before a session exists: auto-create the session on send, or disable with a visible reason. Verify: typing + Enter with zero sessions either sends or explains itself
- [x] 3.5 Shorten the composer placeholder and move the `/` `@` `$` hints out of it. Verify: placeholder stays on one line at 1280 and 960 widths, no clipping
- [x] 3.6 Lock body scroll while a modal is open. Verify: no page scrollbar behind the Configure modal at 800px window height

## 4. Agents, wizard, settings (Wave 3)

- [x] 4.1 Migrate agent cards, avatars, and agent rail rows onto kit (Avatar, SideRow, StatusDot, Chip); drop `card*`/`avatar` daisyUI styling. Verify: agents menu renders in both themes; grep count drops
- [x] 4.2 Migrate the add-agent wizard off `steps`/`fieldset*`/`btn-wizard`/`input-prompt`-era classes onto kit (Segmented, Combobox/ModelCombobox pattern, OptionChips pattern), keeping the ported wizard mechanics intact. Verify: create → generate → edit → regenerate → delete smoke in dev, both themes
- [x] 4.3 Migrate settings panes, forms, provider tiles, and remaining `alert*`/`toggle`/`checkbox`/`loading*`/`progress`/`divider`/`indicator`/`table`/`tab`/`link`/`list-row` call sites. Verify: settings renders in both themes; denylist grep returns 0 in migrated scopes
- [x] 4.4 Agent card content voice: avatar + description/model line replace the truncated ID hash; the hash lives in Configure only. Verify: agents grid shows no raw IDs
- [x] 4.5 Wizard validation renders per-field: error text and error border on the offending input, not under the stepper. Verify: submitting empty step 1 flags the Name field itself
- [x] 4.6 One loading indicator on agent creation — no duplicate spinner in the submit button while the OnboardingLoader is showing. Verify: single spinner during create
- [x] 4.7 Destructive/action voice: card "Delete" is a neutral ghost that takes error only on confirm; provider row actions collapse to a clear hierarchy (primary action + overflow). Verify: no permanent red text actions on cards or rows
- [x] 4.8 Empty states in Skills/Hooks/MCP (and any bare-text pane) use the EmptyState primitive with a next-step action. Verify: no bare-text empty panes remain

## 5. Cutover (Wave 4)

- [x] 5.1 Remove the two `@plugin` daisyUI blocks and the daisyUI theme ramp from `styles.css`; delete retired custom CSS (island/desk/card-lift, coral shadows). Verify: `npm run build` passes; built CSS has no daisyUI output
- [x] 5.2 Remove `daisyui` from package.json, delete `.agents/skills/daisyui` and its `.zcode/skills` symlink; font swap: drop `@fontsource-variable/plus-jakarta-sans`, alias `--font-display` to Inter. Verify: `npm install` clean, `npm run build` passes, no Jakarta reference in src
- [x] 5.3 Rewrite root `design.md` as the onclaw-v2 design contract (genre, tokens, ring grammar, motion, focus, glass re-base) and update the daisyUI line in `openspec/config.yaml` context. Verify: docs read consistent with shipped CSS; `openspec validate --strict` passes for this change
- [x] 5.4 Full gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `vp check` scoped to `frontend/src`, `vitest`, `npm run build` then `task build` (desktop) and `task build:server` (embedded dist). Verify: all gates green; denylist grep returns 0
- [x] 5.5 Pluralize the topbar session count in `App.tsx` ("1 session", "N sessions"). Verify: topbar reads "1 session" with exactly one session

## 6. Audit and verification (Wave 5)

- [x] 6.1 Re-run the hallmark audit over chat, agents, add-agent wizard, and settings using the 2026-10-17 pre-migration punch list (design.md D9) as the baseline — confirm every carried finding is gone, nothing new appeared. Verify: all D9 findings resolved or explicitly waived in the summary; light-theme muted text ≥ 4.5:1 on every surface (contrast gates 40–41)
- [ ] 6.2 Verify `theming` spec scenarios live: toggle without reload from every top-level screen, first-paint default in both system appearances, native chrome (select/scrollbar/selection) matches theme, reduced-motion collapses to opacity, reduced-transparency renders opaque. Verify: each scenario demonstrated in dev or desktop build; issues fixed
- [ ] 6.3 Final both-theme GUI pass over every screen (dev or IAB screenshots), comparing against Wave-0 baseline pairs; record the parity result. Verify: screenshot pairs reviewed per screen; no unstyled or illegible surface in either theme
