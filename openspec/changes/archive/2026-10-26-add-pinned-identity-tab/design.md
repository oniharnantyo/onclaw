# Design

## Context

See proposal.md — Why. The relevant current state, all in `frontend/src/features/chat/`:

- `components/RightBar.tsx` is two shells behind one early-return gate. `if (tabs.length === 0)` renders the identity island — a `glass-panel` aside with `m-2 … hidden xl:flex`, holding `<AgentAvatar … live={live} />` at 64 px, the name, the model line, and the Knowledge opener, or the `KnowledgePanel` when `browsingKnowledge` is set. Otherwise it renders the tab shell: a bordered column (an `absolute … z-30` overlay sheet below `xl`, docked at `xl`) with the `panel-tabs` strip mapped straight from `panelTabs` — each entry a title button plus an `X` close button — and `panel-content` rendering `PANEL_RENDERERS[active.kind]`, where `active = tabs.find(id) ?? tabs[tabs.length - 1]`.
- `browsingKnowledge` is local state reset by an effect on `tabs.length > 0`.
- `useLiveAvatar()` already runs above both early returns so its outcome seen-ref outlives avatar instances across identity ↔ preview swaps (add-live-agent-avatar D8).
- `store.ts` owns `panelTabs` and `activePanelTabID: string | null`. `openPanelTab` appends-and-activates, or raises an existing tab by `identity`; `activatePanelTab(id: string)` sets the id; `closePanelTab` filters and, only if the closed tab was active, raises the last remaining (or `null` when none remain); session switch, agent selection with no session, and the last session's deletion reset both to `[]`/`null`. The invariant today is `activePanelTabID === null ⇔ panelTabs.length === 0`. `RightBar.tsx` is the only reader of `activePanelTabID` outside the store.
- Tests: `__test__/RightBar.test.tsx` already pins "no tab strip with zero tabs", "the SAME aside swaps to tabs", "closing the last tab returns the identity view", "keeps the active tab when another closes", "resets on session switch", the knowledge-panel swaps, and both themes; `__test__/store.test.ts` only resets `panelTabs`/`activePanelTabID` in its fixture.

Constraints: both themes (openspec/config.yaml); the island's render with zero tabs must stay byte-identical — the live-avatar spec's "reduced motion" and "non-identity surfaces" guarantees depend on exactly one live instance; the `tabs.length === 0` grammar is pinned by passing tests that must keep passing unchanged.

## Goals / Non-Goals

**Goals:**

- A visible, always-first way back to the identity view while tabs stay open — the browser pinned-tab idiom.
- Zero tabs renders today's island exactly (markup and classes).
- One source of truth for "what the bar shows", no new persisted state, no new store field.
- At most one live avatar instance at any time.
- Every existing RightBar and store test passes unchanged; new behaviour gets its own cases.

**Non-Goals:**

- Tab reordering, drag, or a close-all control.
- A live mini-face in the strip.
- Keyboard tab cycling; auto-return to the identity on turn end or tab close.
- Showing the identity island below `xl` when no tab is open; persisting the active tab.

## Decisions

### D1 — `null` is the pinned identity tab's id

`activePanelTabID: null` stops meaning only "no tabs" and becomes "the identity is the active surface" — which it already is when no tabs exist. `activatePanelTab` widens to `(id: string | null)`, documented as `null` = the pinned identity tab. Nothing else in the store changes, and the existing reducers already do the right thing under the new reading: `openPanelTab` activates what it opens or raises (a preview opened from the identity tab takes over); `closePanelTab` leaves `activePanelTabID` alone when the closed tab was not active (so closing a background tab keeps the identity showing), raises the last remaining when it was (unchanged), and yields `null` when nothing remains (the island); every reset path already writes `null`.

Alternatives: a separate `identityActive: boolean` — two sources of truth that can disagree (`identityActive && activePanelTabID !== null`); a string sentinel such as `"identity"` kept out of `panelTabs` — every lookup, raise, and close path would have to special-case it, and the close reducer's "was this the active tab" test would need an exception. `null` is already the store's representation of "no preview selected"; the change only lets it coexist with open tabs.

### D2 — The pinned tab is icon-only, first, and never closable

In the tab shell the strip renders, before `tabs.map`, one `<button>` with `data-testid="panel-identity-tab"` — deliberately outside the `panel-tab-` prefix the preview tabs carry and the existing test helpers enumerate, so "the Nth preview tab" keeps meaning what it does today: a static `<AgentAvatar id avatar size={18} />` as its label (no `live`), `aria-label` and `title` set to the agent's name, `aria-current="true"` plus the strip's existing active token classes when it is the active surface, the inactive classes otherwise, and `onClick={() => activatePanelTab(null)}`. It has no close button and nothing can remove it while the strip exists. Like the island, it requires an agent; without one it is omitted and `null` falls through to today's `tabs[tabs.length - 1]` fallback, so the bar is never empty.

Alternative: a text label with the agent's name — rejected because the icon-only treatment is what distinguishes a pinned tab from the closable ones (the same idiom browsers use), it costs ~26 px of a strip that already scrolls horizontally, and the name still reaches assistive technology and the tooltip.

### D3 — The island stays as it is; the identity content becomes a shared element

The `tabs.length === 0` branch keeps its exact markup and classes. The island's inner content — the live 64 px `AgentAvatar`, the name and model paragraphs with their test ids, and the Knowledge opener — moves into a local element (an inline function or a small sibling component in the same file) that both shells render, so the two cannot drift. In the tab shell, `panel-content` renders that element (or the `KnowledgePanel`, D5) when the identity is active and `<Renderer key={active.id} tab={active} />` otherwise.

Alternative: one shell always showing the strip, with the identity as its first tab even when alone — rejected because it changes how the bar looks with zero tabs (the floating glass island with its `m-2` margin becomes a bordered column), changes the `hidden xl:flex` grammar the chat-view spec and existing tests pin, and buys nothing the user asked for.

### D4 — The tab shell derives one flag

`const identityActive = activeStoreID === null && agent !== null;` then `active` is computed only when `!identityActive`, keeping the `find ?? last` fallback for a stale id. The strip's active styling for preview tabs compares against `active?.id`, so no preview tab reads as active while the identity is.

### D5 — Knowledge browsing follows the active surface

The reset effect's trigger changes from `tabs.length > 0` to `!identityActive` — a strict generalisation (today any open tab is the active surface). The Knowledge opener sets `browsingKnowledge` from either shell; the `KnowledgePanel` renders in whichever shell hosts the identity, with the same `onOpenDoc` (opens or raises the document tab, which makes a preview active and so resets browsing) and `onBack`. Returning via the pinned tab after a document opened shows the identity content, not the browser, matching the add-knowledge-corpus rule that the browsing state never survives leaving the identity surface.

### D6 — Exactly one live face, unchanged reaction semantics

`live` from the existing hook (which already sits above the early returns) is passed to the shared identity element's avatar in both shells; the pinned tab's label avatar passes no `live`, so it renders static with no timers or injected styles. While a preview is active the identity element is unmounted and the hook's handle is `null`, so terminal reactions are dropped exactly as today; activating the pinned tab remounts the face and the mount-initialised seen-ref guarantees no replayed gesture. Headless probe: the live wrapper's `data-avatar-state` attribute is present when the identity is shown in either shell and absent while a preview is active.

### D7 — Below `xl` the overlay may host the identity

The island keeps `hidden xl:flex`; the pinned tab renders wherever the strip renders, so in the narrow overlay sheet the user can tap it and see the identity view there. This is a deliberate, small reach the spec delta names in its own scenario. Alternative: hide the pinned tab below `xl` — rejected because the strip would differ by width for no benefit, and the gesture is explicit.

### D8 — Tests

`RightBar.test.tsx`: with one tab open the strip's first child is `panel-identity-tab`, with no `panel-tab-close-*` sibling, and the existing "no tab strip with zero tabs" case still holds; activating it shows `sidebar-agent-name` and a `[data-avatar-state]` wrapper while `panel-tab-<id>` is still listed and `panel-content` no longer renders the preview; opening a new preview or re-activating an open one from the identity tab makes that preview active; closing a non-active tab while the identity is active keeps the identity and the remaining tab; the Knowledge opener works from the identity tab and the browsing state resets when a document tab takes over; the themes case covers the pinned tab's active and inactive classes. `store.test.ts`: `activatePanelTab(null)` with tabs present leaves `panelTabs` intact and `activePanelTabID` `null`; `closePanelTab` of a non-active tab while `null` stays `null`; closing the last tab from a preview still yields `null`.

## Risks / Trade-offs

- [`null` now legitimately coexists with open tabs; code assuming `null ⇒ no tabs` breaks] → the audit shows `RightBar.tsx` is the only reader outside the store; the store's own reducers are correct under the new reading (D1); the type-widened `activatePanelTab` makes the sentinel visible at the call site.
- [Two shells render the identity content and drift] → one shared element (D3); tests assert the content in both shells.
- [The live face with its window-level pointermove listener now reachable in the narrow overlay] → only after an explicit tap, same cost as the docked island, still one instance (D6).
- [The pinned tab steals strip width from titles] → ~26 px; the strip already scrolls horizontally and titles already truncate.
- [Headless smoke cannot exercise the `xl` breakpoint swap] → the existing 1280×800 live-smoke recipe covers the docked state; the overlay case is asserted by unit test on class presence and checked once by hand.

## Migration Plan

None. Pure view state; nothing persisted or migrated. Rollback is a revert of the three source files and two tests.
