# Design

## Context

The audit (2026-11-02, 14/20) ran against the tree including the uncommitted
`add-agent-groups` UI work; fixes land on top of it. Key facts that shape the
design: `ui-conventions` already mandates lifted status-text tiers
(`accenttext`/`warntext`/`dangertext`) and a 24px target floor — the audit's
P1s are largely conformance gaps against synced specs, not missing spec intent.
`--accent-text`/`--danger-text` exist as raw tokens with `@theme` names
`accenttext`/`dangertext`; `--warn-text` does not exist at all. The streaming
path is React-18 + zustand + `@assistant-ui/react` 0.15 external-store: the
adapter rebuilds the whole `ExportedMessageRepository` from the `entries`
array on every delta, and the library's converter cache is WeakMap-keyed on
message identity, so stable per-entry objects make the cache hit. Fonts and
all assets are local; the 6 MB entry cost is parse/compile, not network.

## Goals / Non-Goals

**Goals:**
- Close all audit P1/P2 findings and the agreed P3 tail in one wave.
- Make the status-color grammar enforceable: token-only status hues, vendored
  surfaces included, so the seam stops eroding.
- Keep streaming correctness invariants (first-token-immediately render,
  final-render parity) while bounding per-delta work.

**Non-Goals:**
- No px→rem typography migration, no webview zoom support (deferred; latent).
- No redesign of the sub-xl right-bar overlay or the run-card treatment beyond
  the blur fix.
- No vendored refactor beyond the status-token re-base, the two zoom-dialog
  focus traps, and the typing-indicator unification.
- No Go/backend changes; no bindings regen.

## Decisions

**D1 — Light-usable lifted tiers: `--warn-text`, `--success-text`.**
Add raw tokens to `tokens.css` in both palettes (defined hexes, never aliases
of the fill, per the spec's "MUST NOT resolve by inheriting"):
light `--warn-text: #b45309` (5.02:1 on `--surface`, 4.81:1 on `--bg`),
light `--success-text: #15803d` (5.02:1 / 4.81:1); dark `--warn-text: #eab308`
(9.08:1) and dark `--success-text: #17a34a` (5.28:1) defined as their own
tokens — in dark the fill values already qualify, but the tier is defined
explicitly like `--accent-text`/`--danger-text` are. Map `--color-warntext` /
`--color-successtext` in `theme.css`'s `@theme` block following the existing
`accenttext`/`dangertext` pattern, giving `text-warntext`/`text-successtext`
utilities. Alternative considered: reuse amber-800 `#92400e` (7.09:1) for a
louder light warn — rejected: it drifts from the Tailwind-family hue the
palette-builder palettes already use; amber-700 clears the bar with headroom.

**D2 — Palette re-base mapping.** Mechanical replacement across first-party
and vendored status usages: `emerald-*` → success family (`text-successtext`
for text, `bg-success`/`border-success` fills for strokes and chips,
`text-success` for large glyphs that only need 3:1), `red-*` → danger family
(`text-dangertext` for small text, `text-danger` fills for ≥18px or bold),
`amber-*` → warn family, `blue-*` (progress bar, queue dot) → `accent`.
Vendored `dark:text-emerald-400`-style pairs collapse to the single tier
utility because the tier token flips with `data-theme`. Verification: a
vitest token-contract test greps first-party + `elements/` for
`(emerald|red|amber|blue)-\d` classes and fails on any hit outside the two
allowed vendored shadcn `ui/` primitives if any remain (target: zero).
Alternative considered: leave vendored as-is and re-base only first-party —
rejected: the audit showed first-party already copied the vendored idiom; the
seam erodes again without closing it.

**D3 — Delta coalescing at ingest.** Coalesce `KindTextDelta` (and other
per-token kinds) in the store's ingest path to one commit per animation
frame: incoming deltas accumulate into a pending buffer keyed by entry, a
scheduled flush applies them as one `setState`, and turn-terminal events
flush synchronously so `turn_completed` state changes stay immediate. The
existing `eventBuffer` (page-load replay) stays untouched — this is a new
delta-only path. Alternative considered: throttling at the runtime adapter —
rejected: the store is the single fan-out point (runtime, ContextMeter,
RunSurfaces, useLiveAvatar all read `entries`), so coalescing there fixes
every subscriber at once.

**D4 — Per-entry repository conversion.** Change the runtime adapter so the
library receives stable per-message objects: convert each entry once and
memoize by entry identity (the `entries` array keeps identities stable across
`mergeDelta` — only the tail entry is replaced), passing the converter lane
(`convertMessage`) instead of rebuilding `entriesToRepository`'s output array
per delta. The library's WeakMap cache then hits for unchanged messages and
reconciliation touches only the streaming message. Alternative considered:
memoizing `entriesToRepository` with a custom equality — rejected: still
allocates a new top-level array per delta and re-runs grouping; per-entry
stability is what the cache actually keys on.

**D5 — Narrow the four whole-`entries` subscribers.** RunSurfaces and
ContextMeter derive from scalar slices (running-count, entry-id list, last
turn's tool entries) via narrow selectors; `useLiveAvatar` derives its level
from the active turn's terminal state edge (a small derived store slice)
instead of re-deriving per delta. `useChatRuntime` keeps the `entries`
subscription (it is the adapter) but its re-render cost collapses once D3/D4
bound the work. Markdown stabilization: keep the synchronous parse (the
documented first-token trade) but make `stabilizeMarkdown` incremental —
stabilize only the tail segment since the last stable boundary instead of
rescanning the full text per token.

**D6 — Bundle surgery.** (a) Mermaid: replace the static
`ChatAssistantMessage → mermaid-diagram.aui` import with a lazy boundary;
`MermaidDiagram` renders a code-fence placeholder and dynamically imports the
renderer on mount, so beautiful-mermaid leaves the entry graph entirely.
(b) Markdown chains: make `reasoning.aui` and `task-card.aui` import
`markdown-text` lazily (they render inside already-mounted deferred
surfaces), and route `panelRenderers`' `MarkdownBody` through the same lazy
path — if a chain cannot be broken without behavior risk, delete the dead
`lazy()` in `ChatAssistantMessage` instead and record that markdown rides the
entry bundle (the spec's on-demand requirement covers highlighter + mermaid;
the markdown stack may stay in-entry only if the chains prove unbreakable —
the default is break them). (c) Add `manualChunks` vendor groups (react/react
-dom, radix+base-ui, assistant-ui, markdown stack) so vendor churn does not
invalidate the app chunk. Gate: entry chunk target ≤ ~3.5 MB raw with
mermaid and shiki confirmed absent (grep chunk markers in build output).

**D7 — ARIA grammar per widget.** (a) Button-role lists (agents, providers):
`aria-pressed={isSelected}` replaces `aria-selected` (keeps existing
Enter/Space handling). (b) Tab strips: one shared pattern — each tab gets an
id, `aria-selected`, `tabindex` roving (active 0, others −1), arrow-key
Home/End handling; each panel gets `role="tabpanel"` + `aria-labelledby`.
Apply to settings nav, `AgentConfigPane`, `IdentitySection`,
`DocumentConfigDialog`, and the right-bar preview tabs (preview tabs become
`role="tab"` + selected state; the pinned identity tab keeps
`aria-current` semantics as today). (c) Separators: `tabIndex={0}` +
`aria-valuenow`-style width exposure, ArrowLeft/Right (Left/Up in RTL
agnostic terms: start/end) adjust by the drag increment (4px), Home/End to
clamps; persist on key-release exactly like drag release. (d) StatusDot:
render the dot `aria-hidden` with a visually-hidden sibling label (or
`role="status"` where the state changes live, e.g. running flip) — text, not
`title`. (e) Headings: `ChatHeader` title span → `<h1>` with identical
visual classes; renumber `AgentCard` h3→h2, `AgentConfigPane`/`HooksPane`/
`ChannelsPane` h4s→h3. (f) Skeleton loaders: replace
`aria-label`-on-generic-div with `aria-busy` on the transcript/list
container (announced by SR as busy state).

**D8 — Live transcript region.** Put `role="log"` on the transcript viewport
wrapper (implicit `aria-live="polite"`, insertion-ordered announcements) and
`aria-busy` on it while a turn streams. Alternative considered: a separate
polite summary region announcing completed turns only — rejected: loses
mid-stream visibility, and `role="log"` is the standard pattern for
append-only transcripts; React re-renders of unchanged siblings do not
re-announce under the log pattern. Verify manually that streaming does not
announce storm (polite queue coalesces).

**D9 — Field focus treatment.** Composer root gains the kit field pattern:
`focus-within:border-accent` on the composer shell (the textarea keeps its
input-exception `outline-none`); combobox search input gets `aria-label`
("Search") + `focus:border-accent` directly (it has no wrapper field).
Group headers add `aria-controls` pointing at the existing content region
ids.

**D10 — Polish tail.** (a) Lightbox reuses the kit Modal's Tab-trap approach
(extract the trap into a small shared hook or copy the cycle logic — prefer
extracting `useFocusTrap` from `Modal` so Modal, Lightbox, and the two
vendored zoom dialogs share one implementation). (b) Run card: replace
`backdrop-blur-xl` with a solid `--surface` at contract opacity + ring —
no per-frame backdrop resampling over the scrolling transcript. (c) Typing
indicator: vendored `typing-indicator.tsx` switches its dots to the kit
`.od-dot` classes (drops Tailwind `animate-bounce`; the detector advisory
lands with it). (d) `DocumentConfigDialog` `border-b-2` → hairline
`border-b` per the contract's 1px separation rule. (e) Sub-24px targets:
chip retry/remove, combobox clear, preview-tab close grow to 24px hit areas
(size/padding compensation; glyphs stay small). (f) AvatarPicker: `htmlFor`
/`id` wiring on trait labels, human-readable swatch names + `aria-pressed`,
`aria-expanded` on the Customize disclosure; KnowledgePanel description
input gets `aria-label`.

**D11 — DESIGN.md amendment.** Add `--warn-text`/`--success-text` to the
token inventory with the same "lifted tier" wording as `--accent-text`/
`--danger-text`, and one sentence in the accent section: status hues reach
the UI only through contract tokens — palette utilities must not encode
status colors, vendored surfaces included.

## Risks / Trade-offs

- [Coalescing delays last-token paint by up to one frame] → terminal events
  flush synchronously; a one-frame tail is invisible at streaming rates.
- [Per-entry conversion refactor touches the runtime adapter] → keep
  `entriesToRepository` as the fallback path behind a flag during the change;
  delete it in the same change once parity tests pass (no long-lived flag).
- [Vendored re-base touches ~15 element files] → mechanical class swaps only,
  no structure edits; the token-contract grep test (D2) pins the result so
  drift cannot return.
- [role="log" announce volume on fast streams] → polite queue coalesces;
  verify in the live smoke with a long code answer; if stormy, downgrade to
  per-paragraph announcements (announce only stable blocks) — behavior note,
  no spec change needed ("live log exposes the growing text" stays true).
- [manualChunks regrouping changes chunk hashes] → no dynamic-import graph
  changes besides D6(a/b); run the full vitest suite + build to catch
  chunk-load-order issues; bindings unaffected.
- [`add-agent-groups` UI is uncommitted] → audit fixes land on its state;
  if it changes before apply, re-run the affected greps (file list in tasks).

## Migration Plan

Single frontend wave, no data migration. Land order within the change:
tokens/DESIGN.md (D1/D11) → status re-base (D2) → ARIA sweep (D7/D9) →
live region (D8) → streaming perf (D3–D5) → bundle (D6) → polish tail
(D10) → verification gate. Rollback = revert the commit range; no schema or
API surface touched.

## Open Questions

None — all audit findings map to a decision above; deferred items
(typography scale, sub-xl overlay) are recorded as out-of-scope in the
proposal.
