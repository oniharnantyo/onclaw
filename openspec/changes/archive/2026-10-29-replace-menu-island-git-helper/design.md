# Design

## Context

The nav island is the only chrome surface still rendering its own row grammar:
`App.tsx` builds rows from bespoke `RAIL_ROW*` constants (`h-11`,
`rounded-[12px]`, 19px icons, active = accent tint + accenttext + semibold),
while the sessions rail beside it uses the shared kit — `SectionLabel` headings
over `SideRow` rows (`h-[30px]`, `rounded-md`, `px-2.5`, active = accent 15%
tint with default foreground). git-helper's left menu (the reference look) is a
flat grouped menu of compact rows with a tinted selection pill — which is the
grammar `SideRow` already implements. All four scope forks were decided by the
user (see proposal.md): keep small icons, no headings, pin the grammar in
`app-shell`, visual-only scope elsewhere.

Constraints: the rail header's `pt-6` clears the macOS invisible-titlebar drag
band (first 25px — nothing interactive may sit there); `App.test.tsx` pins the
collapsed rail's contract (icon-only, no "Agents"/"Schedules" text, buttons
reachable by accessible name); both themes and both hosts (desktop webview,
server-mode web) render the same frontend.

## Goals / Non-Goals

**Goals:**

- One row grammar across the whole left edge: nav island rows adopt `SideRow`.
- git-helper's menu feel: compact flat rows, tinted selection pill, quiet
  bottom cluster — with the user's keep-icons deviation.
- Spec-pin the resulting grammar (app-shell ADDED requirement).

**Non-Goals:**

- No group headings, no re-grouping of destinations (user decision).
- No change to the collapsed 80px rail's behavior or the below-lg mobile top
  bar (their markup and classes stay as-is).
- No sessions-rail, window-chrome, or backend changes; no new dependencies.

## Decisions

- **D1: Adopt `SideRow` for the nav rows instead of restyling the bespoke
  constants.** `SideRow` already matches git-helper's geometry and active
  state, and the sessions rail proves it in both themes. Restyling
  `RAIL_ROW*` in place would leave two live grammars to keep in sync.
  *Alternatives:* bespoke restyle (rejected: duplicate grammar); import antd's
  Menu like git-helper (rejected: foreign dependency, the kit already has the
  grammar).
- **D2: `SideRow` grows an optional `testId` prop, nothing else.** The
  Settings row must keep its `rail-settings` testid (smoke tooling and
  `App.test.tsx` use it); `SideRow` currently emits only `data-od-id`. The
  prop defaults to undefined so existing sessions-rail usage is untouched.
  Accessible naming keeps coming from the existing `title || label` fallback;
  if the collapsed-rail `getByRole(name)` lookups ever miss, add an optional
  `ariaLabel` the same way rather than changing `SideRow`'s default behavior.
- **D3: Icons stay in expanded rows at 16px (user decision).** git-helper is
  text-only, but the icons aid scanning and are required in the collapsed rail
  anyway; shrinking 19px → 16px fits the 30px row and sits optically next to
  the sessions rail's 18px avatars. The `MenuItem` icon field is unchanged.
- **D4: Active state becomes the SideRow pill (user decision: no headings,
  git-helper selection).** Accent-tint background with default foreground
  text; the accent-text + semibold active styling retires from the rail
  branch. `RAIL_ROW_ACTIVE`/`RAIL_ROW_IDLE` stay defined — the below-lg bar
  branch still uses them and is out of scope.
- **D5: Bottom cluster aligns to the 30px row.** `ThemeCycleButton`'s
  rail-expanded branch drops `h-11` → `h-[30px]` and `gap-3` → `gap-2`,
  keeping its state-bearing icon and the exact `Theme: <pref>` label/aria
  contract; the icon-only collapsed variant is untouched. The Settings row
  becomes a `SideRow` with `testId="rail-settings"` (D1/D2). The `border-t`
  separator stays as the cluster's only division (no headings per the user).
- **D6: Everything around the menu is frozen.** Rail header + collapse toggle
  (and the `pt-6` drag-band clearance), rail widths (`w-56`/`w-20`), mobile
  bar, and strip overlays keep their current markup; only the two nav clusters'
  rows change.
- **D7: The rail renders as a flat canvas column — no card of its own (user
  decision, 2026-10-28: "keep on current place, just remove the island
  border").** git-helper's menu sits flush on the window canvas with no
  bordered container; the rail's `glass-chrome`, `border-line`, `bg-surface`,
  and `rounded-lg` classes come off the `aside` so the left edge reads as one
  continuous menu surface, and the content island remains the carded surface.
  Drag-band clearance (`pt-6`), widths, and collapse behavior are untouched;
  the below-lg header keeps its card (out of scope). `App.test.tsx`'s
  `railAside()` relocates the rail by a new `data-testid="nav-rail"` (the old
  `aside.glass-chrome` selector died with the card).
  *Alternatives:* also de-carding the sessions rail so nav + sessions read as
  one flat sidebar (offered, user skipped — literal single-column scope
  chosen; revisit if the flat column reads unfinished next to the carded
  sessions panel); keeping the card and only softening its border (rejected:
  keeps the two-grammar chrome the user asked to remove).

## Risks / Trade-offs

- [Collapsed accessible names come from `title` via SideRow, not the explicit
  `aria-label` used today] → `title` yields the same accessible name for
  buttons; `App.test.tsx`'s `getByRole("button", { name })` lookups verify
  this. Fallback documented in D2.
- [Losing accent-colored active text reduces contrast of the active marker in
  low-vision conditions] → the pill is the same 15% accent mix the sessions
  rail ships in both themes; weight/semantics still conveyed by `aria-current`
  (kept on every row).
- [Two icon sizes now exist on the left edge (16px nav vs 18px session
  avatars)] → deliberate: 16px reads as chrome, 18px as content identity; if
  it optically clashes in review, bump to 18px — a one-token change.
- [Spec pins a visual grammar, which future redesigns must delta] → that is
  the point per the user's app-shell decision; the requirement is written as
  observable behavior (rows, pill, accessible names), not class names.

## Migration Plan

Single frontend change, no data or config migration. Land as one commit
(`feat(frontend)` scope per the repo's commit protocol); rollback is reverting
the commit. `vp check src` + the `App.test.tsx` suite are the gates; live
verification is `task dev` + screenshots delivered via the PDF panel (desktop
expanded, collapsed, and dark/light).

## Open Questions

None — all scope forks were resolved by the user before capture.
