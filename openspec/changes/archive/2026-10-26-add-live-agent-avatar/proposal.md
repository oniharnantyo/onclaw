# Proposal

## Why

Every agent in buddies has a face — the procedural avatar the wizard's picker produces, or the id-derived one — but it is a still picture. The app's premise is a buddy that keeps working in the background, yet the one place its face sits while it works, the right bar's identity view, shows the same frozen expression whether the agent is idle, reasoning, running a tool, waiting for an approval, or has just failed. Two things make this the moment: the right-bar merge left exactly one in-chat identity surface to animate, and `@oniharnantyo/react-nice-avatar` — this project's fork of the avatar library — shipped today with an `agentState` vocabulary (`thinking`, `speaking`, `tool_call`, `listening`, `error`, …) that lines up one-to-one with the turn states the chat store already tracks.

## What Changes

- **Library swap, drop-in.** `react-nice-avatar@^1.5.0` is replaced by `@oniharnantyo/react-nice-avatar@^0.1.0` at every import — the kit `Avatar`, `lib/avatar.ts`, `components/types.ts`, the picker and agent panes, and four tests. The fork exports the same `genConfig`, `AvatarFullConfig`, `NiceAvatarProps`, and style unions as supersets; for object configs its `genConfig` sets no key that was not passed, so `lib/avatar.ts`'s pinned deterministic faces and every stored `Agent.Avatar` config render exactly as before. Not breaking.
- **A pure avatar-state derivation.** A selector beside the run island's model projects the active session onto the fork's vocabulary: no turn → `idle`; sent but no event yet → `listening`; running with no output, or reasoning streaming → `thinking`; text streaming → `speaking`; a tool running at the tail → `tool_call`; an unresolved approval → `listening` with a question badge; an inline error → `error`. Level states derive from the session's entries, so a replayed transcript rests on the same state the live view ended in.
- **Live reactions on terminal outcomes, live only.** `turn_completed` nods with a sparkle, `turn_cancelled` shakes with a sweat drop, `turn_error` holds the error face until the next user message. These fire from the live ingest path and never from a replay.
- **The identity view's avatar comes alive.** The right bar's 64 px face renders animated: ambient blink, saccades, and sway; gaze that follows the pointer across the window; dozing off after idle inactivity and waking — startled — when a turn starts or the pointer returns.
- **Reduced motion renders static.** The fork never injects its keyframes under `prefers-reduced-motion: reduce`; the live wrapper additionally withholds the live props under the app's existing `useReducedMotion()`, so the identity face is a still picture there, as ui-conventions requires.
- **Everything else stays still.** Agent cards, the list column, the config pane, scheduler rows, and the picker preview pass no live props and render as today — no timers, no injected styles.

Out of scope for this change: a live face in the transcript's turn-activity line or anywhere below the `xl` breakpoint where the identity view is hidden; ambient life on agent cards or list entries; a live picker preview or exposing the fork's new trait values (frown, squint, raised brows, …) in the Customize grid; a persisted resting expression or mood per agent; the `sentiment` channel driven by tool outcomes; mouth movement timed to speech audio; per-message avatars in the transcript.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-panels`: "Right panel shell" gains a companion requirement — the identity view's avatar reflects the active session's turn state, reacts to live terminal outcomes, idles with ambient life, and renders static under reduced motion and after a transcript reload.
- `agent-management`: "Agent list layout" names the upstream package in its requirement text; it now describes the entry's generated avatar without pinning the package, since the app's avatar component is backed by the fork. The behaviour it requires — a generated avatar per entry, the stored config winning over the id-derived face — is unchanged.

## Impact

- **Frontend only.** `frontend/package.json` (dependency swap); `components/ui/Avatar.tsx` (an optional `live` prop passed through to the fork, with the wrapper's `overflow-hidden` clip relaxed on the live path so badges and head sway are not cut); `components/AgentAvatar.tsx` and `components/types.ts` (pass-through); `lib/avatar.ts` (import); `features/agents/components/{AvatarPicker,AgentConfigPane,CreateAgentModal}.tsx` (imports); a new `features/chat/run/avatarState.ts` selector with its `__test__`; `features/chat/store.ts` (a last-turn-outcome field set only on live ingest); a thin `useLiveAvatar` hook; and `features/chat/components/RightBar.tsx` (the identity view mounts the live face). Four existing tests change their import path. Both themes: the face's own colours are config-driven and theme-independent; the badge overlays must be checked in both.
- **No backend, bindings, event kind, or migration.** `Agent.Avatar` stays opaque JSON with the same keys.
- **Dependencies:** one swap. The fork's runtime deps (`chroma-js`, `prop-types`) are the upstream's minus `@babel/runtime`; unpacked size roughly doubles (296 KB → 621 KB). Its React peer range covers 18.2.
- **Specs:** two delta specs, no new spec.
- **Runtime cost:** one animated instance with a window-level, 60 ms-throttled `pointermove` listener while the identity view is shown; nothing on any other surface.
