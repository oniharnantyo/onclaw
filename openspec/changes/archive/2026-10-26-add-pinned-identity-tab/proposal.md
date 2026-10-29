# Proposal

## Why

The right bar is strictly two-state: the identity view (the agent's live face, name, and effective model) renders only while zero preview tabs are open, and the first tab — a subagent trajectory from the run island, a file chip, a knowledge document — replaces it until every tab is closed. Since add-live-agent-avatar that identity view is the session's status surface, and the turns that open tabs are exactly the subagent-heavy ones where the user wants to keep several trajectories open and still glance back at the face; today the only way back is to close all of them.

## What Changes

- **A pinned identity tab leads the strip.** Whenever the tab strip is shown (one or more tabs open), its first entry is a pinned tab for the session's identity: the agent's small avatar as its label (name in the tooltip and accessible label), no close affordance, never reordered or removed. Activating it shows the identity view inside the same bar — the live face, name, model line, and Knowledge opener — while every open tab stays open in the strip.
- **"No tab selected" becomes a real state.** The store's `activePanelTabID: null` already means the identity is showing when no tabs exist; it now also means "the pinned identity tab is active" while tabs exist, so one value drives both shells and nothing new is persisted.
- **Everything else keeps today's rules.** Opening or re-activating a preview still activates that preview (the identity is replaced in place); closing the active tab still raises the last remaining tab; closing the last tab still returns the plain identity island with no strip; switching sessions still resets to the identity view. Closing a non-active tab while the identity tab is active leaves the identity showing.
- **Knowledge browsing stays identity-local.** The Knowledge opener works from the identity tab too; the browsing state resets whenever the identity stops being the active surface, generalising today's "any tab open resets browsing" rule.
- **Exactly one live face.** The identity view's 64 px avatar is live wherever the identity view is shown — the island or the identity tab — through the existing `useLiveAvatar` hook; the pinned tab's small label avatar renders static. Terminal reactions that arrive while a preview tab is active are still dropped, so returning via the pinned tab plays no gesture.
- **Narrow widths gain a way to the face.** Below `xl` the island stays hidden as today, but the tab state's overlay sheet now hosts the identity view when the pinned tab is active — the user asked for it by tapping the tab.

Out of scope: reordering or dragging tabs; a close-all action; a live mini-face in the strip; keyboard shortcuts for cycling tabs; returning to the identity automatically when a turn ends or a tab closes; showing the identity island below `xl` without tabs; persisting the active tab across session switches or restarts.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-panels`: "Right panel shell" gains the pinned identity tab — the strip leads with a non-closable identity tab that returns the bar to the identity view without closing any tab — while its existing open/replace/close/reset rules stay as written. "Identity view live avatar" changes its precondition from "when no tab is open" to "whenever the identity view is shown" (island or identity tab), adds the pinned tab's label avatar to the surfaces that MUST render static, and notes that returning via the pinned tab plays no deferred reaction.

The chat-view "Chat right sidebar" requirement stays accurate as written — the identity shows whenever the bar holds no preview, opening a preview replaces it in place, and closing the last preview restores it — so it carries no delta; the return affordance is additive and belongs to the tab strip chat-panels owns.

## Impact

- **Frontend only.** `features/chat/types.ts` and `features/chat/store.ts` (`activatePanelTab` accepts `null` for the pinned identity tab; no new field, no new action); `features/chat/components/RightBar.tsx` (the strip renders the pinned tab, the tab shell's content area renders the identity view when it is active, the knowledge browsing reset follows the active surface, and the identity content moves into a local element shared by both shells — the island's markup and classes are unchanged when no tab is open). Tests: `features/chat/__test__/RightBar.test.tsx` and `features/chat/__test__/store.test.ts` extend; every existing case keeps passing unchanged.
- **No backend, bindings, event kind, or migration.** Pure view state; nothing persisted.
- **Both themes:** the pinned tab reuses the strip's existing active/inactive token classes; the identity content inside the tab shell reuses the island's text tokens.
- **Specs:** one delta spec (`chat-panels`, two MODIFIED requirements), no new spec.
- **Runtime cost:** unchanged — still at most one animated avatar instance; the static label avatar has no timers or injected styles.
