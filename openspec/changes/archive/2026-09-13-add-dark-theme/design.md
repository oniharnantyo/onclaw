# Add Dark Theme — Design

## Context

The frontend renders every color through one indirection chain: component → Tailwind v4 utility (`bg-surface`, `text-fg`, `border-line`) → `web/src/styles/theme.css` `@theme` mapping → raw CSS custom properties in `web/src/styles/tokens.css`. A repo-wide grep confirms near-total tokenization: hex literals exist only in `AvatarPicker.tsx` (decorative avatar colors), and the only palette-class usage is ~11 `text-white` button labels on accent/danger fills. `index.html` loads the app module with no pre-paint theming. Nothing in the codebase reads `prefers-color-scheme` today (the `prefers-reduced-motion` block is the OS-respect precedent). The nav rail (`Rail.tsx`) renders a bottom cluster of Settings + user avatar in both shapes (68px collapsed / 200px expanded); the mobile drawer reuses it. See proposal.md for motivation, specs/web-app/shell/spec.md for the behavior contract.

## Goals / Non-Goals

**Goals:**
- Dark theme with zero per-component color work: one token override tier.
- Theme control reachable everywhere the app renders chrome, including pre-auth.
- No light-flash on reload; correct theme at first paint.

**Non-Goals:**
- No Settings-page appearance section (user decision: sidebar-only access).
- No theme control on the BootError screen (it inherits the stored theme, offers no control).
- No per-workspace or server-synced theme; no `prefers-color-scheme` for anything other than theme.
- No redesign of either palette — light stays byte-identical to the contract.

## Decisions

**D1 — Attribute token swap, not Tailwind `dark:` variants.** Dark mode is a `data-theme="dark"` attribute on `<html>` plus a `:root[data-theme="dark"]` override block in `tokens.css`. The `dark:` class-variant strategy was rejected: components never author colors, so variants would buy nothing and permanently double the authoring burden for every future component. Derived tokens (`--accent-hover` color-mix, `--elev-raised` fg-mixed shadow, scrollbar, selection, focus ring) adapt automatically because they compute from variables.

**D2 — Dark palette (pinned).** Neutrals invert the contract's own values — the contract's ink `#111111` becomes the canvas:

| Token | Light (frozen) | Dark |
|---|---|---|
| `--bg` | `#fafafa` | `#111111` |
| `--surface` | `#ffffff` | `#1a1a1a` |
| `--surface-warm` | alias → surface | `#222222` (becomes a real tier) |
| `--fg` | `#111111` | `#ededed` |
| `--fg-2` | alias → fg | `#c9c9c9` (becomes a real tier) |
| `--muted` | `#6b6b6b` | `#9a9a9a` |
| `--border` | `#e5e5e5` | `#2c2c2c` |
| `--border-soft` | alias → border | `#232323` (becomes a real tier) |
| `--accent`, `--accent-on`, status hues | unchanged | unchanged |

The dormant alias tiers in `tokens.css` ("default has no X tier") exist as theming room and are activated by the dark block. Accent stays brand-pure: white-on-accent labels = 4.56:1 ✓; accent-as-text on dark ≈ 4.14:1 (just under AA 4.5); `text-danger` ≈ 3.9:1. A lifted `--accent-text` tier (`#6b93f2`, 6.3:1) is the ready remedy via the same alias mechanism if the visual pass flags real usages — deferred, not pre-applied.

**D3 — Preference engine as a standalone module.** New `web/src/lib/theme.ts`: pure resolution logic (`stored | null` + OS scheme → `light | dark`) plus a small controller that writes `data-theme` on `documentElement`, persists to a single namespaced local-storage key following existing app conventions, and subscribes to `matchMedia('(prefers-color-scheme: dark)')` changes only while the preference is system (live-tracking per spec). It is deliberately not part of the zustand chat store — theme is app-level UI state with no relation to threads/sessions, and a standalone module is importable from the pre-React bootstrap script's sibling logic and testable without a store.

**D4 — No-flash bootstrap inline in `index.html`.** A ~6-line inline script before the module tag: read the storage key, resolve against `matchMedia`, set the attribute. It duplicates the resolver in miniature; the key name and resolution rule are pinned by a unit test on `theme.ts` and a cross-referencing comment in both files so they cannot drift silently.

**D5 — One cycle control, three mount points.** A shared `ThemeCycleButton` renders the sun/moon/monitor icon for the current state plus click-cycles light → dark → system → light. The rail mounts it in the bottom cluster above Settings using the existing rail row pattern (expanded: icon + "Theme: <mode>" label; collapsed: 44px icon button + right tooltip announcing current state and next selection). `LoginView` mounts the icon-only variant absolutely in the viewport's top-right corner with a bottom tooltip. New `sun`, `moon`, `monitor` icons join the `Icon.tsx` catalog in the existing stroke style. The `Segmented` control is not used (that pattern fits form fields, not a one-click cycle).

**D6 — Semantic sweep.** The ~11 `text-white` labels on `bg-accent`/`bg-danger` buttons become `text-accenton` (settings panes, `ToolCall.tsx`, `AgentCard.tsx`, `admin/UsersPane.tsx`). No visual change in light; it routes labels through the one token that would flip if a theme ever needed a different on-color.

## Risks / Trade-offs

- [Accent/danger text contrast in dark is marginally under AA for normal-size text (4.14:1 / 3.9:1)] → Visual pass inventories real accent-as-text and danger-as-text usages; the `--accent-text`/status alias tiers are the one-line remedy if needed. Accepted for v1 otherwise.
- [Dark values are first-cut, not prototype-derived (no dark prototype exists)] → Full-screen dual-theme screenshot sweep (Playwright, existing harness) plus a manual pass over chat, tool cards, context meter, mention pills, markdown code blocks, schedules, runs, settings, admin, login, boot error; palette deltas land as token edits only.
- [Bootstrap script drifts from `theme.ts` resolver] → Key name and rule pinned by unit test; cross-reference comments in both files.
- [`matchMedia` listener API differences in older browsers] → Feature-detect `addEventListener` with `addListener` fallback, same as common practice; theme still correct on reload regardless.

## Migration Plan

Frontend-only and additive; no data, API, or schema changes. Deploy is a normal web build. Rollback is revert. Stored preference keys in local storage are inert if the feature is removed.

## Open Questions

None blocking. Status-color tuning in dark and any lifted accent-text tier are explicitly deferred to the visual pass, which lands as token-file edits without touching the task breakdown.
