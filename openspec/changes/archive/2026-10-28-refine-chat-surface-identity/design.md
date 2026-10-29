# Design

## Context

The chat surface's mechanical layer is clean (detector: 1 advisory across
245 files; zero raw hex; lifted status tiers used correctly), but a
dual-agent critique (25/40) and audit (13/20) located its gaps in identity
and state semantics: unattributed turns, untitled-looking session rows,
raw errors as turn endings, visual-only rail state, and a composer rail
that overflows at ordinary widths (documented at ~1440px in the light-theme
smoke capture). Every finding was tested against the spec corpus during
exploration; the deltas above carry only what is genuinely new behavior.
The backend already titles sessions on first dispatch
(`internal/agents/runner.go`), so this change needs no backend work beyond
one window-options constant.

## Goals / Non-Goals

**Goals:**

- The conversation and session list carry the same companions-with-reach
  identity the rebuilt rail and right bar already have.
- Failures end turns with a human voice and a working recovery path.
- Rail state is legible to assistive technology and meets target-size
  floors.
- The composer rail survives any window width the user can produce.

**Non-Goals:**

- No backend changes beyond the window minimum size (no new event fields,
  no service surfaces, no bindings regen).
- No right-bar companion panel (needs a memory service surface and origin
  persistence — its own future change).
- No turn-origin marks in the transcript (`sessions.Event` carries no
  Origin field; future `mark-turn-origin` change).
- No auto-title work — titling exists; legacy backfill is the separate
  `backfill-session-titles` change.
- No estimate-caption changes (the popover's per-row "estimated" captions
  are spec-mandated by `chat-view` "Chat right sidebar").
- No vendored-file edits (`assistant-ui` elements stay untouched per the
  established D13 rule).

## Decisions

- **D1 — Meta line is time-only, right-aligned, same-height.** The row
  keeps its 26px density; `relativeTime` (`frontend/src/lib/relativeTime.ts`,
  built for exactly this and dropped by the rail redesign) renders as the
  kit `SideRow`'s `sub` slot. A preview line or message count would need
  new backend data (`Session` has no preview field) — rejected for this
  wave. Alternative considered: two-line rows — rejected: doubles the
  rail's height for 37-session users.

- **D2 — Turn attribution is a static 16px avatar + name stub per
  assistant turn.** `useLiveAvatar` is architected as the right bar's one
  live face (a single imperative handle owns the nod/shake reactions);
  mounting a second instance would make two faces fight over reactions.
  Static attribution delivers the identity fix with zero fork risk.
  Subagent correctness verified during exploration: subagent activity
  renders inside Task tool cards, so "attribution = session agent" is
  always true at message level. Compaction markers return before the stub.

- **D3 — Error card classifies on the frontend, honestly.** Conservative
  pattern classes (connection refused/dial tcp → provider unreachable;
  401/403/api key → credentials; context/token limit → context window)
  map to plain headlines; anything unrecognized gets "This turn failed."
  The raw string always survives in a collapsed mono details disclosure.
  Retry calls the message's existing regenerate path, composing with the
  shipped branch rules (failed regeneration keeps the prior variant —
  `chat-view` "Regenerate and branch variants"). The classifier is a named
  bridge: `classify-turn-errors` (backend, typed `ErrorKind` at the turn
  loop terminal via the codebase's existing `errors.As` idiom) retires it.

- **D4 — The composer distills by yield, not removal.** The `/ @ $` hint
  row renders only while the draft is empty (it is discoverability chrome
  for the trigger popovers, which keep working); the model trigger shows
  the family name — the last `/`-delimited segment of the model id, via a
  shared `shortModelName` helper — with the full id in the picker menu and
  the tooltip; the right bar's model line adopts the same family label
  with the provider display name. The "default" markers stay (they carry
  the override/inherit state). Evidence: the wrap reproduces at ~1440px in
  the light-theme smoke capture; family name + yield removes ~150px of
  resting chrome.

- **D5 — Window floor measured, then enforced.** `MinWidth`/`MinHeight`
  land in `main.go`'s `WebviewWindowOptions` after the distilled rail is
  measured at the narrowest supported layout (both side bars at their
  minimum drag widths). Placeholder 960×600; the task includes the
  measurement, not a guess.

- **D6 — A11y semantics at the kit level.** `SideRow` gains
  `aria-current` (from `active`) and an optional `aria-expanded`
  (accordion headers pass it; plain rows omit it). Icon targets move to
  24px by enlarging the hit area, not the glyph. The context ring's tier
  text classes move to the lifted variants (`accenttext`/`warntext`/
  `dangertext`) — same grammar the links and error rows already use.

- **D7 — Archive ordering.** The MODIFIED sessions requirement is based
  on `add-grouped-session-rail`'s delta text (the post-archive state) so
  this change's archive cannot regress that change's wording. This change
  archives after it.

- **D8 — Motion is a grammar, not moments (folded in on user request).**
  Structural chrome — nav rail collapse, session column reveal, accordion
  expand/collapse, right bar minimize/restore, identity↔preview swap —
  shares one duration and easing: `--motion-base` (200ms) with
  `--ease-standard`; micro-feedback keeps `--motion-fast`. The accordion
  reveals via `grid-template-rows 0fr→1fr` (no raw-height animation); the
  chevron's existing 200ms rotation syncs with it. The right bar's
  collapsed state changes from `return null` (hard unmount) to a
  zero-inline-size keep-mounted state so minimize/restore animates and the
  identity content survives the round trip — safe because its hooks are
  already remount-safe, and strictly stronger than today. Drag-resize
  disables the transition for 1:1 pointer tracking and re-arms on release.
  Reduced motion needs no new code: the global contract (ui-conventions
  "Reduced-motion compliance") already collapses every transition to
  opacity-only ≤150ms, which snaps these layout moves — the correct
  fallback. Layout-property honesty: a reflowing column cannot be faked
  with transforms, so the transitions animate one layout property
  (inline-size / grid-rows) per container at 200ms on a desktop webview —
  the measured, bounded case the motion guidance allows.

## Risks / Trade-offs

- [Static avatars read as less alive than the critique's "reusing
  useLiveAvatar" sketch] → accepted: one-live-face is a correctness rule,
  not a style preference; the right bar keeps the live moment.
- [Frontend error classifier misclassifies an unrecognized eino string] →
  the fallback headline is honest ("This turn failed") and the raw line is
  always one disclosure away; the backend follow-up retires the guessing.
- [Composer yield could surprise someone who learned the hints as
  permanent] → the hints return the moment the draft empties; the trigger
  popovers themselves are unchanged.
- [Spec drift: the rail change is complete-but-unarchived] → D7 pins the
  base text; apply must not edit the same requirement wording in main
  specs.
- [Six prototype edits already in the working tree (SideRow aria,
  SessionRail meta/expanded/24px) are unverified] → tasks.md carries them
  as done-pending-verification; the wave's gate reruns the full frontend
  suite over them.

## Migration Plan

Frontend-only plus one Go constant; no data migration. Rollback is a
revert of the working tree. Bindings are untouched (no service changes),
so no regen step and no `task build:server` interaction.

## Open Questions

- None material. The exact minimum-size numbers are measured during apply
  (D5); the classifier's pattern list grows only if a real error class
  shows up mis-headlined.
