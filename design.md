# Design — buddies

Adopted from **onclaw-v2** at full parity (change
`replace-daisyui-with-styled-components`, 2026-10-17). This file is the locked
design contract: every UI change reads it before emitting code, and extends or
amends it instead of inventing per page. The source of truth is the shipped
CSS — `frontend/src/styles/tokens.css`, `frontend/src/styles/theme.css`, the
base/motion section of `frontend/src/styles.css`, and the kit in
`frontend/src/components/ui/`.

## What the system is

onclaw-v2's neutral-minimal assistant-UI language: neutral grey surfaces, a
single blue accent, hairline separation, no decoration. daisyUI is gone — the
coral Hallmark palette, Plus Jakarta Sans, and the island/desk/card-lift
elevation grammar retired with change `replace-daisyui-with-styled-components`.

- **Surfaces:** page `--bg`, panels `--surface` (`--surface-warm` is a real
  second tier in dark only). Flat color + a 1px hairline ring
  (`--elev-ring: 0 0 0 1px var(--border)`); islands, desk tints, and
  hover-lift shadows are retired. The only shadow tiers are `--elev-raised`
  (2px/8px at 8% ink) and the focus ring.
- **Accent:** single blue `#2f6feb` (`--accent`, `--accent-on: #ffffff` as its
  label color; hover/active derive via `color-mix` toward black 8%/14%).
  Status hues are the only other colors: `--success: #17a34a`,
  `--warn: #eab308`, `--danger: #dc2626`. `--brand: #d97757` exists solely as
  the theme-invariant brand-mark fill (`lib/brandMarks.tsx`) — it is not a UI
  color.
- **Shape:** radii 8/12/16 px (`--radius-sm/md/lg`) plus `--radius-pill`.
- **Motion:** 150 ms (`--motion-fast`) for hover/focus/micro-states, 200 ms
  (`--motion-base`) for general state changes, eased with
  `--ease-standard: cubic-bezier(0.2, 0, 0, 1)`.
- **Type:** 12–64 px scale (`--text-xs` 12 → `--text-4xl` 64), Inter
  everywhere (`--font-display` **aliases** Inter — the display/body split is
  weight plus `--tracking-display: -0.01em` applied at sizes ≥32 px, not a
  second family). JetBrains Mono (`--font-mono`) for code, model lines, and
  token counts. Fonts are self-hosted via `@fontsource-variable/*` — the
  desktop app must work offline.

## Token inventory

Raw tokens live in `frontend/src/styles/tokens.css` (`:root` + a
`:root[data-theme="dark"]` override block); `theme.css` maps them onto
Tailwind v4 `@theme` names so utilities resolve to the contract.

| Raw token (`:root`, light → dark) | Tailwind `@theme` name | Utility examples |
|---|---|---|
| `--bg` `#fafafa` → `#111111` | `--color-bg` | `bg-bg` |
| `--surface` `#ffffff` → `#1a1a1a` | `--color-surface` | `bg-surface` |
| `--surface-warm` alias → `#222222` (dark only) | `--color-warm` | `bg-warm` |
| `--fg` `#111111` → `#ededed`; `--fg-2` alias → `#c9c9c9` | `--color-fg`, `--color-fg2` | `text-fg`, `text-fg2` |
| `--muted` `#6b6b6b` → `#9a9a9a`; `--meta` alias | `--color-muted`, `--color-meta` | `text-muted`, `text-meta` |
| `--border` `#e5e5e5` → `#2c2c2c`; `--border-soft` alias → `#232323` | `--color-line`, `--color-linesoft` | `border-line`, `border-linesoft` |
| `--accent` `#2f6feb` (both themes) | `--color-accent` | `bg-accent`, `text-accent` |
| `--accent-on` `#ffffff`; `--accent-hover`/`--accent-active` (`color-mix` toward black) | `--color-accenton` | `text-accenton`, `hover:bg-[var(--accent-hover)]` |
| `--accent-text` alias → `#6b93f2` (dark lifted tier) | `--color-accenttext` | `text-accenttext` |
| `--success` `#17a34a` / `--warn` `#eab308` / `--danger` `#dc2626` | `--color-success/warn/danger` | `bg-success`, `text-danger` |
| `--danger-text` alias → `#f87171` (dark lifted tier) | `--color-dangertext` | `text-dangertext` |
| `--font-body` / `--font-display` (both Inter Variable) / `--font-mono` (JetBrains Mono Variable) | `--font-sans`, `--font-mono` | `font-sans`, `font-mono` |
| `--radius-sm/md/lg` 8/12/16 px, `--radius-pill` | `--radius-sm/md/lg` | `rounded-sm`, `rounded-md`, `rounded-lg` |

Non-color raw tokens without a `@theme` mapping are consumed directly by CSS
and kit components: the type scale (`--text-*` 12–64 px, `--leading-body`
1.5 / `--leading-tight` 1.2, `--tracking-display` −0.01em), the 4-point spacing
ladder (`--space-1` 4 px → `--space-20` 80 px), elevation (`--elev-flat: none`,
`--elev-ring`, `--elev-raised`), the focus ring
(`--focus-ring: 0 0 0 3px color-mix(in oklab, var(--accent), transparent 70%)`),
motion (`--motion-fast`/`--motion-base`, `--ease-standard`), and `--brand`.

**shadcn seam.** `theme.css` also carries two `@theme inline` blocks resolving
shadcn-standard names (`bg-background`, `text-muted-foreground`,
`border-border`, `ring-ring`, `bg-primary`, `bg-destructive`, …) onto the raw
contract tokens, plus the genuinely missing raw names (`--card`, `--popover`,
`--radius: 12px`, `--aui-*` tunables, `--chart-1..5`) for vendored registry CSS
that calls `var(--card)` directly. `inline` is load-bearing: utilities carry
the raw var itself, so the `:root[data-theme="dark"]` overrides apply with no
duplicated dark palette. The `dark:` variant is bound once via
`@custom-variant dark (&:is(:root[data-theme="dark"] *))` — it follows the app
theme, never `prefers-color-scheme`.

**Import-order caveat.** `--font-mono` and `--radius-*` map to raw tokens of
the same name; the `@theme` output is self-referential
(`--font-mono: var(--font-mono)`) and only resolves because
`tokens.css` is imported after `@import "tailwindcss"` (see
`frontend/src/styles.css`). Do not reorder those imports without giving the raw
tokens distinct names.

## Component architecture

- **`cn()`** (`frontend/src/lib/cn.ts`) = `clsx` + `tailwind-merge`; the kit
  composes variant chains with it. No CSS-in-JS, no styled-components, no
  ThemeProvider — tokens stay CSS-first so `data-theme` re-skins without a
  React re-render and the native/glass layers keep reading raw variables.
- **The kit** (`frontend/src/components/ui/`, Base-UI-free — behavior-heavy
  components hand-roll their interactions with native ARIA semantics):
  Alert, Avatar, Button, Chip, Combobox, Icon, Input, MicroLabel, Modal,
  OptionChips, SectionLabel, Segmented, Select, SideRow, Skeleton, StatusDot,
  Textarea, ThemeCycleButton, Toasts, Toggle, Tooltip. Each has a vitest suite
  under `__test__/`. `Icon` draws Lucide-style 24×24 strokes inline (keyed by
  name) so the kit stays dependency-free; `lucide-react` may appear at feature
  call sites — one stroke voice either way.
- **Simple elements are kit primitives in the onclaw idiom.** onclaw-v2
  inline-styles buttons/inputs at call sites; buddies wraps those exact utility
  chains into kit components instead of repeating them per screen. New UI
  composes from the kit; feature-specific styling stays feature-local, never
  new global classes.
- **8-state discipline** on every interactive primitive: rest / hover / active
  / focus-visible / disabled / loading, with the two themes riding the tokens
  (no per-theme component code). Disabled and loading collapse into the
  `disabled` attribute with `aria-busy`; the kit adds no second rings.

## Focus contract

`:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }`
globally (base section of `styles.css`). Text inputs and textareas opt out
locally (`outline-none`) and take the focus-within border treatment on their
parent field instead. No component adds a second ring or shadow on top — one
focus voice, instant (never animated). The `--focus-ring` token exists for the
rare box-shadow-based case; it is the same accent at 30%.

## Motion primitives

Defined in the base section of `frontend/src/styles.css`:

- `.od-pop` — enter: opacity 0→1 + 8px rise/scale 0.985, 200 ms
  `--ease-standard`.
- `.od-fade` — plain opacity crossfade, 200 ms.
- `.od-caret` — blinking accent caret (900 ms `step-end`).
- `.od-dot` — typing indicator: three 5px dots bouncing (1.2 s, staggered).
- `.od-shimmer` — text shimmer sweep (1.8 s linear loop).
- `.od-live` — success pulse ring (`--success` at 45%, 2 s loop).
- `.od-scroll` — thin scrollbar, `--fg` at ~16% thumb over a transparent
  track.

Under `prefers-reduced-motion: reduce` everything collapses to opacity-only
fades of at most 150 ms: `transition-property: opacity !important`, durations
capped at 150 ms, animation iteration 1 — no transform travel, no loops,
instant focus rings. Reduced motion is an OS setting; the app honors it
globally, not per component.

## Theme engine

`frontend/src/lib/theme.ts` — a standalone module (not zustand-coupled):

- **Three-state preference** `light | dark | system`, persisted under
  `THEME_STORAGE_KEY = "buddies-theme"`; stored `light`/`dark` overrides the
  OS, `system` (and anything unrecognized) resolves via
  `matchMedia("(prefers-color-scheme: dark)")` and keeps following OS changes
  live until overridden.
- **`data-theme` on `<html>`** is the contract: `resolveTheme`,
  `cyclePreference` (light → dark → system), `applyTheme`, `initTheme`
  (applies immediately, subscribes to OS changes while `system`, returns a
  cleanup).
- **No-flash bootstrap:** an inline script in `frontend/index.html`
  duplicates the resolve rule in miniature and sets `data-theme` before the
  app module executes — keep it in sync with `THEME_STORAGE_KEY` +
  `resolveTheme`; `theme.test.ts` pins the correspondence.
- **Native chrome:** `color-scheme` follows the applied theme, so native
  controls (selects, checkboxes, scrollbars) match; components rely on it
  (the kit `Select` ships appearance-default on purpose). `WindowThemeService`
  (root `windowtheme*.go`) keeps the macOS window appearance — traffic lights
  and frost — in sync with the applied theme.
- **Reduced transparency:** under
  `prefers-reduced-transparency: reduce` the glass layer renders opaque (see
  below) — the fallback branch is the real design, not a degraded copy.

## Liquid glass (macOS desktop)

Desktop-native; no onclaw-v2 counterpart. `stores/platform.ts` sets
`html[data-glass]` only in the Wails desktop app on darwin (main.go's
`MacBackdropLiquidGlass` window) — browsers, the server frontend, and other
OSes never match and keep the opaque theme. The layer is kept from the
previous design but re-based on the neutral tokens; every tint is a
`color-mix` over a contract token, so both themes stay readable:

| Layer | Tint (over backdrop blur) |
|---|---|
| `.glass-chrome` (title bar + rail) | `var(--surface)` 58% · blur 28px |
| `.glass-surface` (work surface) | `var(--bg)` 80% · blur 18px |
| `.glass-panel` (inner panels) | `var(--surface-warm)` 30% · blur 28px |
| `.glass-overlay` / modals | `var(--bg)` 72% · blur 32px |

The native glass material follows the **system** appearance (not the app
theme); `WindowThemeService` forces the NSWindow appearance to match the app
theme so frost and traffic lights track it. Under reduced transparency all
layers restore solid `--bg`/`--surface`/`--surface-warm` with blur disabled.

## Hallmark's narrowed role

Hallmark is discipline, not design source (design decision D7 of the change):
the system is adopted, so no new theme invention — no new hues, ramps, or
elevation ideas outside this file. What still runs: the component-state
discipline (8 states on interactive primitives, instant `:focus-visible`),
locked tokens (no raw hex in `frontend/src` outside tests — `--brand` is the
single brand-mark exception), the implementation-agnostic slop gates (honest
copy, no invented metrics, no re-drawn chrome), and periodic audits over chat /
agents / add-agent wizard / settings, with fixes made **inside** this
contract.
