# Proposal

## Why

daisyUI is already load-bearing in name only: it is loaded with `themes: false`, so it supplies
component classes while the visual identity lives in the project's own CSS and the `design.md`
system. The user has now directed (2026-10-16, twice, escalating) that buddies **follow the
predecessor onclaw-v2 design system** — confirmed as full parity: onclaw-v2's token contract, its
shadcn-style component kit, and its visual language. daisyUI (and the coral hallmark palette with
it) gives way to that system, which also resolves the earlier styled-components question: onclaw-v2
uses none — its kit is Tailwind-utility components with `cn()`, so the port brings that
architecture instead.

## What Changes

- **Port the onclaw-v2 design contract**: `styles/tokens.css` (raw `:root` /
  `[data-theme="dark"]` tokens — neutral grey surfaces, single blue accent `#2f6feb`, 8/12/16 px
  radii, hairline-ring elevation, 150/200 ms motion, 12–64 px type scale) and `theme.css`
  (`@theme` var mapping onto Tailwind v4 utilities). Token names replace daisyUI's
  (`--color-base-*` → `--bg/--surface/--fg/--muted/--line/--accent/…`).
- **Port the component architecture**: `cn()` (clsx + tailwind-merge), a `src/components/ui/`
  kit in the onclaw-v2 style — porting what exists there per need (Avatar, Chip, MicroLabel,
  SectionLabel, Segmented, SideRow, StatusDot, Toasts, Toggle, Tooltip, Modal, Combobox family)
  and adding the simple primitives onclaw-v2 inline-styles (Button, Input, Alert, Skeleton, …)
  in the same idiom. Base UI (`@base-ui/react`) where behavior is complex, as onclaw-v2 does.
  **BREAKING (code-contributor API only)**: `className="btn …"`-style daisyUI classes stop
  carrying styles; ~55 files migrate to kit components + Tailwind utilities.
- **BREAKING (visual)**: the warm-coral playful identity is replaced — Plus Jakarta Sans display
  face dropped (Inter everywhere), island desk grammar retired in favor of onclaw-v2's flat
  surface/ring grammar, accent discipline now onclaw-v2's. The macOS liquid-glass layer stays
  (desktop-native, no onclaw-v2 counterpart) but is re-based on the new tokens.
- **Theme engine parity**: port `lib/theme.ts` — three-state preference (light/dark/system)
  with OS-following and the inline no-flash bootstrap in `index.html`; `data-theme` on `<html>`
  stays the contract.
- **Remove daisyUI**: drop the devDependency, the `@plugin` lines, and the daisyUI skill
  (`.agents/skills/daisyui` + its `.zcode/skills` symlink).
- **Rewrite root `design.md`** as the onclaw-v2 design contract (the locked system this project
  defers to), and update the `openspec/config.yaml` context line that says styling uses daisyUI.
- **Hallmark role narrows to discipline**: no new theme invention (the system is adopted, not
  designed); hallmark's component discipline (8 states, tokens-only, slop gates) governs the
  rebuild, and a hallmark audit + fix pass covers chat, agents, add-agent wizard, and settings.

Sequencing: captured 2026-10-16; implementation starts after `port-add-agent` archives (its
remaining tasks are the both-theme GUI pass and full gates over the same screens).

## Capabilities

### New Capabilities
- `theming`: the theme mechanism as observable behavior — dual themes (light + dark) switchable
  at runtime via `data-theme` without reload, system-preference resolution (including an explicit
  "system" choice that tracks OS changes), native chrome following the active theme, and opaque
  rendering under reduced transparency. Pins the behavior the migration must preserve and future
  styling work must hold.

### Modified Capabilities
- `chat-view`: the Theme parity requirement says "both the light and dark daisyUI themes" — reworded
  to implementation-neutral "light and dark app themes" (behavior unchanged).
- `agent-management`: same daisyUI wording in its Theme parity requirement.
- `settings`: same daisyUI wording in its Theme parity requirement.

## Impact

- **Dependencies**: `frontend/package.json` — `+clsx`, `+tailwind-merge`, `+@base-ui/react`,
  `-daisyui`; fonts: `-@fontsource-variable/plus-jakarta-sans` (Inter + JetBrains Mono stay).
- **Styling core**: `frontend/src/styles.css` loses both `@plugin` blocks and the coral ramp;
  gains the ported token contract. Custom CSS that outlives it: liquid-glass tints (re-based),
  focus/selection/scrollbar rules re-pegged to new tokens; island/desk/card-lift CSS retires.
- **Call sites**: ~55 files under `frontend/src` migrate daisyUI class strings to kit components
  and Tailwind utilities; shared primitives in `frontend/src/components/` re-base on the kit.
- **Untouched**: Go backend, Wails bindings, service APIs. `frontend/src/stores/theme.ts`
  migrates to the ported three-state engine (same `data-theme` contract).
- **Build/runtime**: no CSS-in-JS runtime (pure Tailwind output); server binary embeds `dist`,
  so binaries need a rebuild to show the new CSS.
- **Docs/specs**: root `design.md` rewritten; `openspec/config.yaml` context; the three
  theme-parity requirement wordings above.
- **Verification**: `vp check` scoped to `frontend/src`, `vitest`, `npm run build` → `task build`,
  both-theme GUI pass including liquid-glass (`html[data-glass]`) and reduced-motion checks.
