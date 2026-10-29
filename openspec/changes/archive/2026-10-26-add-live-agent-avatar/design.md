# Design

## Context

See proposal.md — Why. The pieces this design composes already exist:

- **Kit avatar.** `components/ui/Avatar.tsx` renders image → generated face → initials, pins `shape="rounded"` on the fork's own knob, and wraps the face in `overflow-hidden rounded-md`. `components/AgentAvatar.tsx` picks the stored config over `lib/avatar.ts`'s id-derived one. `lib/avatar.ts` pins every key `genConfig` would otherwise random-fill, because a partial config re-rolls the face per mount.
- **Turn signals in the store.** `features/chat/store.ts` holds `activeTurnID`, `dispatching`, and the per-session `runningSessions` flag; `makeIngest(sessionID)` is the live event path, where `isTerminalKind` retires the running flag and an early return keeps background sessions' events away from the viewed transcript. `entries.ts` folds events into `entries` replay-exactly (`finalize()` on terminal kinds, an inline `error` entry on `turn_error`, approval entries resolved in place). `TurnActivity.tsx` already reads the tail of the in-flight turn (no output yet / text or reasoning streaming / tool running / approval pending) — the same derivation this change needs, trapped in render logic.
- **Run island precedent.** `features/chat/run/islandModel.ts` is a pure projection of `entries` with table tests, "so live events and a replayed transcript rebuild identical sections."
- **Identity view.** `RightBar.tsx` renders `<AgentAvatar size={64} />` with name and model when no tab is open (`hidden xl:flex`); opening a preview or the knowledge panel swaps the island out in place, which unmounts the avatar.
- **Reduced motion.** `features/chat/tools/useReducedMotion.ts` exists; `ui-conventions` "Reduced-motion compliance" requires all app motion to collapse to ≤150 ms opacity changes.
- **The fork (`@oniharnantyo/react-nice-avatar@0.1.0`), verified against its published bundle.** Props: `animated`, `idle`, `agentState`, `sentiment`, `fx`, `gesture`, `lookAt` (`none|hover|global|center`), `talking`, `secondaryMotion`, `sleepAfter`; a ref handle with `nod/shake/sneeze/squash/doubleTake/talk/turn/showFx/clearFx/wake/sleep`; a `useAvatarController` hook. For object configs `genConfig` sets `expression`/`animated`/`idle` only when passed. In `render()` the expression resolves `flash → asleep → drowsy → sentiment → agentState → config`, so sleep out-ranks the agent state. `componentDidUpdate` handles `gesture`, `fx`, `animated/idle` flips, and `lookAt` rebinding — a change of `agentState` alone neither wakes a dozing avatar nor re-arms its sleep timer; only pointer activity, tab visibility, and the public `wake()` do. The sleep timer arms only when `animated` or `idle` is set, after `sleepAfter ?? 60000` ms. `lookAt="global"` binds a window `pointermove` listener (60 ms throttle) and unbinds on unmount or prop change. Keyframes are injected lazily and never under `prefers-reduced-motion: reduce`; `window.matchMedia` is guarded. React peer `>=16.8 || ^18 || ^19`.

Constraints: frontend only; both themes; reduced motion compliant; a replayed transcript must never react; surfaces that are not the identity view must cost nothing.

## Goals / Non-Goals

**Goals:**

- One pure, table-tested derivation from store state to the fork's vocabulary, consumed by a thin hook — no fork knowledge outside `Avatar` and that hook.
- Replay-exact resting state: a reloaded transcript shows the face the live view ended on, and nothing moves on reload.
- Live-only reactions, dropped (not deferred) when the face is unmounted.
- The static surfaces render identically to today, with no timers and no injected styles.
- Integration through documented props and ref methods only; never through the fork's internal state.

**Non-Goals:**

- Driving the avatar through `useAvatarController` end to end (it owns imperative state and forces `animated`/`lookAt` defaults; it fits apps without a store).
- A sentiment layer from tool outcomes, speech-timed mouth movement, or any second live surface.
- Changing what the picker lets a user choose, or persisting an expression.

## Decisions

### D1 — Explicit import rename, not a package alias

Every `from "react-nice-avatar"` becomes `from "@oniharnantyo/react-nice-avatar"` and `package.json` swaps the dependency. Ten files, mechanical, grep-able, and honest about which package the app runs.

*Alternative:* an npm alias (`"react-nice-avatar": "npm:@oniharnantyo/react-nice-avatar@^0.1.0"`) — zero source diff, but it hides the dependency behind the upstream's name while the two APIs are already diverging. Rejected.

### D2 — `Avatar` grows an optional `live` prop; absent means today's render

`AvatarProps.live?: LiveAvatarProps` carries `agentState`, `fx`, `lookAt`, `sleepAfter`, and the ref. When present, `Avatar` adds `animated`, `secondaryMotion`, and those props to the fork element and drops `overflow-hidden` from its wrapper (keeping `rounded-md`; the fork's `shape="rounded"` clips the face itself, and the badges and head sway must be free to leave the 64 px box). When absent, the fork element receives exactly the props it does today, so it arms no timer and injects no style — the fork gates both on `animated`/`idle`/`fx`/a non-idle state. `AgentAvatar` forwards `live` unchanged; every other call site passes nothing.

*Alternatives:* a separate `LiveAgentAvatar` — duplicates the image/face/initials fallback chain; an always-animated `Avatar` — puts a timer on every card in the grid and changes agent-management surfaces the proposal keeps still. Both rejected.

### D3 — A pure selector over store state is the single source of the level state

`features/chat/run/avatarState.ts` exports `deriveAvatarState({ entries, activeTurnID, dispatching }) → { agentState, fx }`, resolved in precedence order:

1. `dispatching` (sent, no event yet) → `listening`.
2. `activeTurnID` null → resting. Walking `entries` from the tail: an `error` entry met before any `user` entry → `error` (the fork supplies the exclamation badge); otherwise `idle`.
3. In flight, over the active turn's entries from the tail: an unresolved `approval` → `listening` + `fx: "question"`; an `error` → `error`; a `tool` or `task` entry with running status → `tool_call`; a streaming `assistant` → `speaking`; a streaming `reasoning` → `thinking`; anything else (no output yet, between steps, a plan or compaction entry) → `thinking`.

Everything here is a function of `entries`, which the fold rebuilds identically on reload — so "error holds until the next user message" is a derivation, not a remembered flag, and a reloaded transcript whose last turn failed rests on the error face exactly as the live view did. Subagent delegation needs no case: the parent's tail is the delegating tool entry, running. The `activeTurnID` gate means stale running markers from a turn that died without a terminal event cannot animate a reloaded session.

*Alternatives:* deriving from assistant-ui's thread like `TurnActivity.tsx` — couples the selector to aui part shapes and leaves the error-sticky rule to a second source; a store field updated by the ingest switch — imperative, a second truth beside `entries`, and not replay-safe. Rejected.

### D4 — Reactions are edges from the live ingest path only

`store` gains `lastTurnOutcome: { turnID, kind, at } | null`, set in `makeIngest` for terminal kinds **after** the active-session guard (a background session's outcome never moves the viewed face), reset to null on session switch beside the panel reset, and null on load. The hook reacts to a change of that object's identity: `turn_completed` → `ref.nod()` + `showFx("sparkle", ~2 s)`; `turn_cancelled` → `ref.shake()` + `showFx("sweat", ~2 s)`; `turn_error` → `ref.shake()` (the level state already shows the error face). The hook records the outcome it last reacted to in a ref initialised to the current value on mount, so a remount, a session switch, or a reload never replays a gesture; a reaction that fires while the avatar is unmounted (preview tab open) finds a null ref and is dropped.

*Alternatives:* timestamps in `entries` with a "within 2 s" window — needs a clock and reacts to replayed recent events; the declarative `gesture` prop — replays only on value change, so two consecutive nods need a key dance. Rejected.

### D5 — Wake on turn start; keep the doze

Because sleep out-ranks `agentState` in the fork and a prop change does not wake it, the hook calls `ref.wake()` when `dispatching` turns true or `activeTurnID` goes from null to a value. Between turns the fork's own cycle runs: `sleepAfter` ≈ 2 minutes, yawn, droop, sleep; pointer or visibility wakes it with the startle flash. Dozing off when left alone is on-brand for a buddy and costs nothing.

*Alternatives:* disabling sleep — there is no knob; `0` sleeps immediately and anything above 2³¹−1 ms overflows `setTimeout`. Pinning a large-but-safe value is possible but gives up a behaviour the proposal wants. Rejected.

### D6 — Gaze follows the pointer across the window

`lookAt="global"` on the identity face. The fork binds its window listener only while a mounted instance carries that prop and unbinds on unmount — the cost exists only while the identity view is on screen (no tab open, `xl` and up), and it is one throttled listener.

*Alternative:* `hover` — the face only looks back when the pointer crosses 64 px, which in practice is never. Rejected.

### D7 — Reduced motion: the face is simply not live

`useLiveAvatar` returns `undefined` when `useReducedMotion()` is true, so the identity face renders through the static path like every other surface. The fork already withholds its keyframes under the media query, but its expression glides and gaze transforms may be CSS transitions rather than keyframes; withholding `live` makes ui-conventions compliance independent of fork internals.

*Trade-off:* under reduced motion the face carries no state cue; the transcript's turn-activity line (already static there) carries it.

### D8 — The hook owns the ref and the effects; `RightBar` only mounts

`useLiveAvatar()` (beside the selector) reads `entries`, `activeTurnID`, `dispatching`, and `lastTurnOutcome` through `useChatStore` selectors, runs `deriveAvatarState`, holds the ref, runs the D4 and D5 effects, and returns `live` (or `undefined` under D7). `RightBar`'s identity branch becomes `<AgentAvatar … live={live} />`. The hook lives in `RightBar`, which stays mounted across identity ↔ preview swaps, so D4's seen-ref persists while only the avatar instance comes and goes.

### D9 — Tests

`__test__/avatarState.test.ts` table-tests every row of D3 over synthetic entries, plus the replay cases (reloaded failed turn → `error`; reloaded completed turn → `idle`; a user message after an error clears it) and the approval case. The `Avatar` test gains a case that `live` passes the fork props through and drops the clip, and one that its absence renders the same element as today. The hook test uses fake timers to assert nod/shake calls on outcome transitions, no call on mount, and `wake()` on turn start. The four existing tests change only their import path; their renders stay static, so the fork's timers never run under jsdom there.

## Risks / Trade-offs

- [The fork's glides or gaze survive its keyframe gate under reduced motion] → D7 withholds `live` entirely; compliance does not depend on fork internals.
- [Badges or head sway clip, or the `idea`/`exclamation` badge collides with the name line under the 64 px face] → D2 drops the clip; the island already has `p-4` and `gap-3`; if a badge still touches the name, add top margin on the identity island — a tuning, not a design change. Check in both themes on the glass panel.
- [A dozing face receives a turn] → D5 wakes it; failing that, the first pointer movement does (fork behaviour).
- [A stale gesture on mount, remount, or session switch] → D4's seen-ref initialised to the current outcome, plus the reset on session switch.
- [A background session's terminal event moves the viewed face] → D4 sets `lastTurnOutcome` after the active-session guard.
- [`wake()` may not re-arm the doze timer] → the face stays awake until the next pointer movement re-arms it; acceptable.
- [A window-level pointer listener on a desktop app] → one instance, 60 ms throttle, unbound while any tab is open; if it ever shows in a profile, fall back to `hover` by changing one prop.
- [The fork is 0.1.0 and its API may move] → caret-pinned; the integration touches only documented props and ref methods; `Avatar` and the hook are the only two files that know the fork's live API.
- [Unpacked size roughly doubles] → desktop bundle; accepted.
- [`genConfig`'s return type claims `expression`/`animated`/`idle` are present while the runtime omits them] → nothing in buddies reads those keys and the picker's persisted output is unchanged; noted for anyone touching the picker.

## Migration Plan

1. Swap the dependency and rename the imports; `vp check src` and the suite stay green with no visual change anywhere — a safe intermediate commit.
2. Add the selector and its tests, the `lastTurnOutcome` field, `Avatar`'s `live` path, the hook, and the `RightBar` wiring.
3. Live smoke in the desktop app: a turn that reasons, runs a tool, and streams text to completion; an approval wait; a cancel; a forced error followed by a reply; open and close a preview mid-turn; switch sessions; toggle system reduced motion; both themes.
4. Rollback: revert; no persisted data changed. Finish with `task build`.

## Open Questions

- Exact timings (sparkle and sweat durations, the doze delay) — visual tuning settled in tasks.
- Whether, under reduced motion, a static per-state expression could be passed once a smoke confirms the fork snaps rather than glides when its keyframes are withheld — still "static" under the spec, so deferrable.
- Whether `dispatching` earns a small `nod` ("got it") on top of `listening` — tuning.
