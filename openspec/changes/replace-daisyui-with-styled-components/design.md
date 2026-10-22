# Design

## Context

daisyUI 5 runs as a Tailwind v4 plugin with `themes: false` — component classes only; the coral
identity lives in two custom `daisyui/theme` blocks in `frontend/src/styles.css`. Theme switching
is a `data-theme` attribute on `<html>`; no JS imports daisyUI; ~55 files use daisyUI class
families. The predecessor `onclaw-v2` (source of the add-agent port) ships a mature design system
for the same assistant-ui stack: `web/src/styles/tokens.css` (raw tokens, `:root` +
`:root[data-theme="dark"]`), `theme.css` (`@theme` var mapping), `lib/theme.ts` (three-state
preference + OS-following + no-flash bootstrap), and a `components/ui` kit (Avatar, Chip,
MicroLabel, SectionLabel, Segmented, SideRow, StatusDot, Toasts, Toggle, Tooltip, Modal,
Combobox family — simple elements like buttons/inputs are utility-styled at call sites; Base UI
under behavior-heavy components). The user directed full parity with that system. See proposal.md.

## Goals / Non-Goals

**Goals:**
- buddies' frontend adopts the onclaw-v2 design contract — same tokens, same architecture,
  same visual language — so components and patterns port between the two repos with minimal
  re-styling.
- A tree that compiles at every commit: daisyUI stays installed until each family is migrated;
  removal is the last step.
- The migration is pinned by the `theming` spec: runtime switching, no-flash first paint,
  native-chrome parity, reduced-transparency opacity.

**Non-Goals:**
- No feature or layout restructuring — the Workbench rail/surface/inspector structure and all
  service behavior stay; only the visual/component layer changes.
- No daisyUI compatibility shim: daisyUI class names are not re-declared; call sites migrate.
- No CSS-in-JS, no component runtime beyond what onclaw-v2 uses.
- The macOS liquid-glass layer stays (desktop-native feature) but is not redesigned — only
  re-based on the new tokens.

## Decisions

### D1 — Port the token contract wholesale, same names and values
`tokens.css` + `theme.css` port over with their values (neutral surfaces, blue accent, status
hues, 8/12/16 radii, ring elevation, 150/200 ms motion, 12–64 px scale) and the same
raw-token→`@theme` indirection onclaw-v2 uses — including its documented caveat: same-name
mappings (`--font-mono`, `--radius-*`) rely on import order, which must not change. daisyUI token
names (`--color-base-*`, `--radius-box/field/selector`) retire; the utilities that referenced
them (`text-base-content/60`, `bg-base-200`, `rounded-box`) are rewritten at call sites to the
new vocabulary (`text-muted`, `bg-surface`, `rounded-md`, …). buddies' coral ramp and
`--ease-hallmark`/`--shadow-island` tokens retire with design.md's rewrite.

### D2 — shadcn-style kit + `cn()`; no styled-components, no ThemeProvider
Components are TSX + Tailwind utilities composed with `cn()` (clsx + tailwind-merge); Base UI
provides behavior for combobox/tooltip-class components, as in onclaw-v2. Theme tokens stay
CSS-first — `data-theme` re-skins without a React re-render, and the glass/native layers keep
reading raw variables.

### D3 — Kit lives in `frontend/src/components/ui/`; port by need, add in-idiom what's missing
Port onclaw-v2 components as call sites demand (StatusDot, Segmented, Toggle, Tooltip, Toasts,
Modal, Combobox family, MicroLabel, SectionLabel, Chip, SideRow, Avatar…). Where onclaw-v2
inline-styles simple elements (buttons, inputs, alerts), buddies adds small kit components in the
same idiom rather than repeating utility chains 36× (`Button`, `Input`, `Alert`, `Skeleton`).
`ModalShell`/`ConfirmDialog` keep their public APIs and re-base on the ported Modal.
buddies-specific styling (agent cards, session rail) rebuilds on the kit — feature-local, not
new global classes.

### D4 — Per-family migration with daisyUI removed last
Each wave: port/refresh the kit pieces → migrate their call sites → verify → move on. The
daisyUI plugin stays in `styles.css` until the cutover wave so unmigrated screens never restyle
mid-flight. The distinct-class inventory from exploration is the checklist; a grep denylist over
`frontend/src` (no `btn`, `badge`, `modal-box`, … usage) gates the cutover.

### D5 — Theme engine port, including the no-flash bootstrap
`lib/theme.ts` ports as a standalone module (not zustand-coupled): three-state preference,
`resolveTheme`/`cyclePreference`, OS-following via `matchMedia`, storage key, and the inline
bootstrap in `index.html` that sets `data-theme` before first paint (pinned by its test).
`stores/theme.ts` either wraps it or is replaced by it; consumers keep the same `data-theme`
contract, and `WindowThemeService` sync survives (it reads the applied theme, not the engine).

### D6 — Root `design.md` is rewritten as the onclaw-v2 contract
The locked system flips: genre becomes onclaw-v2's neutral-minimal voice (Inter everywhere,
tracking −0.01em ≥32px), surface/ring elevation grammar replaces islands, accent discipline and
focus-ring contract (`:focus-visible` 2px accent outline + 3px ring tokens) port verbatim,
motion primitives (od-pop/od-fade/caret/dots/shimmer/live) port with reduced-motion kill-switches.
Liquid-glass tints re-derive from the neutral surfaces; the fallback branch stays the real
design. `openspec/config.yaml`'s daisyUI context line updates.

### D7 — Hallmark is discipline, not design source
The system is adopted, so hallmark's catalog/custom machinery stays idle. What still runs: the
component-state discipline (8 states on every interactive primitive, instant `:focus-visible`),
the slop gates that are implementation-agnostic (honest copy, no invented metrics, no re-drawn
chrome, locked tokens), and a final `hallmark audit` over chat / agents / add-agent wizard /
settings with fixes inside the adopted contract.

### D8 — Fonts: same faces as onclaw-v2, buddies' variable builds
Inter and JetBrains Mono stay via `@fontsource-variable/*` (same faces as onclaw-v2's static
builds, one file per face, offline-safe); `plus-jakarta-sans` drops. `--font-display` aliases
Inter — the display/body split is weight and tracking, not a second family.

### D9 — Pre-migration audit punch list is the port's acceptance baseline
A hallmark audit ran 2026-10-17 against the daisyUI build (all screens, both themes, 1280×800;
mechanical gates clean — no transition-all, no hover-scale, no italic, no gradients, no emoji
icons, single Lucide voice, cursor conventions correct). Findings split two ways:

- **Dissolved by the port** (do not fix in place): the light-theme muted-ink contrast failure
  (`text-base-content/60` at 4.28/4.14/3.94:1 — below the 4.5 floor on every light surface) dies
  with the coral tokens; onclaw-v2's muted token clears 4.5:1 on its surfaces. Wave 5 must verify
  the ported values actually do (gates 40–41). Same for the double focus ring, ghost-as-label
  affordance, detached validation, and modal re-centering — these live in layers the kit rebuilds,
  so the fixes are acceptance criteria for the new components, not patches to daisyUI-era code.
- **Carried per-task** (would port forward as slop if untouched): "1 sessions" pluralization,
  hash-as-subtitle agent cards, bare-text empty panes, composer's silent no-op + instruction
  placeholder, double spinner on agent creation, permanent red Delete link on cards, provider
  row's flat text-action hierarchy, raw brand hex, missing modal scroll lock, theme-toggle label
  ambiguity, clipping composer placeholder. Each has a checkbox in its wave (tasks 1.6–6.1).

## Risks / Trade-offs

- [Largest call-site churn yet: ~55 files, class strings → components + renamed tokens] →
  per-family waves, each ending green (`vp check`, vitest, build); grep denylist gates cutover.
- [Visual regression versus today's screenshots] → expected and intended (parity is the change);
  before/after screenshot pairs per wave make drift reviewable instead of accidental.
- [Liquid glass reads wrong on neutral surfaces] → re-derive `--glass-*` tints during the shell
  wave; verify on real macOS light and dark system appearance (the native material follows the
  system, not the app theme).
- [assistant-ui chat surfaces styled differently than onclaw-v2's port] → onclaw-v2's
  assistant-ui integration styles are the reference; port rather than re-invent.
- [Missed daisyUI class renders unstyled] → Wave 0 grep script becomes a standing check; final
  both-theme GUI pass walks every screen.
- [FOUC if the bootstrap and engine drift] → port `theme.test.ts`'s index.html assertion with it.
- [Wizard screens (add-agent) restyle mid-flight] → wizard wave lands after the kit waves;
  OnboardingLoader's onclaw-v2 provenance makes the target state already familiar.

## Migration Plan

1. **Wave 0 — foundation + pilot**: port tokens.css/theme.css/cn()/theme engine + bootstrap;
   retire nothing yet; pilot migrates one screen region (e.g. StatusDot + a button cluster) to
   prove dev, prod, `vp check`, both themes, glass fallback.
2. **Wave 1 — shell**: rail/topbar/panes off daisyUI menu/badge classes onto kit + new tokens;
   island grammar → ring grammar; glass tints re-based.
3. **Wave 2 — chat**: transcript, composer, session rail, tool cards (assistant-ui styling port).
4. **Wave 3 — agents + wizard**: agent cards, avatars, add-agent wizard (Segmented, Combobox,
   OnboardingLoader patterns), settings panes/forms.
5. **Wave 4 — cutover**: remove `@plugin` lines + daisyUI dep + skill files; grep denylist gate;
   rewrite design.md; update `openspec/config.yaml`; full `npm run build` → `task build`.
6. **Wave 5 — hallmark audit & gates**: audit key screens, fix within contract, verify `theming`
   scenarios (switch, first paint, native chrome, reduced motion/transparency), final both-theme
   GUI pass.

Rollback: waves land as independent commits; reverting the cutover commit restores daisyUI
output while ported kit components (which don't depend on it) keep working.

## Open Questions

None — remaining choices (exact wave composition per screen, whether `stores/theme.ts` wraps or
is replaced) are implementation-level and belong to their waves.
