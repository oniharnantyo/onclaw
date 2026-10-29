# Tasks

## 1. Dependency swap (D1)

- [x] 1.1 In `frontend/package.json`, replace `react-nice-avatar@^1.5.0` with `@oniharnantyo/react-nice-avatar@^0.1.0`
- [x] 1.2 Rename every `from "react-nice-avatar"` import in `frontend/src` to `from "@oniharnantyo/react-nice-avatar"` — `components/ui/Avatar.tsx`, `components/types.ts` (which `components/AgentAvatar.tsx` consumes), `lib/avatar.ts`, `features/agents/components/AvatarPicker.tsx`, `features/agents/components/AgentConfigPane.tsx`, `features/agents/components/CreateAgentModal.tsx`, and the tests `lib/__test__/avatar.test.ts` and `features/agents/__test__/AvatarPicker.test.tsx`
- [x] 1.3 Verify `cd frontend && npx tsc --noEmit` passes and the four avatar-related test files (`lib/__test__/avatar.test.ts`, `features/agents/__test__/AvatarPicker.test.tsx`, `components/ui/__test__/Avatar.test.tsx`, `components/__test__/AgentAvatar.test.tsx`) stay green with no visual change on any surface

## 2. Pure selector (D3)

- [x] 2.1 Create `frontend/src/features/chat/run/avatarState.ts` exporting `deriveAvatarState({ entries, activeTurnID, dispatching }) → { agentState, fx }`, resolved in D3's precedence order: `dispatching` (sent, no event yet) → `listening`; `activeTurnID` null → walking entries from the tail, an `error` entry met before any `user` entry → `error`, otherwise `idle`; in flight, over the active turn's entries from the tail → an unresolved `approval` → `listening` + `fx: "question"`, an `error` → `error`, a `tool`/`task` entry with running status → `tool_call`, a streaming `assistant` → `speaking`, a streaming `reasoning` → `thinking`, anything else → `thinking`
- [x] 2.2 Create `frontend/src/features/chat/run/__test__/avatarState.test.ts` table-testing every precedence row plus the replay cases (a reloaded failed turn rests on `error`, a reloaded completed turn rests on `idle`, a user message after an error clears it) and the approval case

## 3. Store field (D4)

- [x] 3.1 Add `lastTurnOutcome: { turnID, kind, at } | null` to `ChatStoreExtras` in `frontend/src/features/chat/store.ts`, set only on the live ingest path (`makeIngest`) for terminal kinds after the active-session guard, reset to null on session switch beside the panel reset, and null on load
- [x] 3.2 Extend `frontend/src/features/chat/__test__/store.test.ts` to cover set-on-terminal, untouched by a background session's terminal event, and reset on session switch

## 4. Avatar live prop (D2)

- [x] 4.1 Add the optional `live?: LiveAvatarProps` prop (agentState, fx, lookAt, sleepAfter, ref handle) to `frontend/src/components/ui/Avatar.tsx`: when present, add `animated`, `secondaryMotion`, and the live props to the fork element and drop `overflow-hidden` from the wrapper keeping `rounded-md`; when absent, render exactly today's element
- [x] 4.2 Pass `live` through in `frontend/src/components/types.ts` and `frontend/src/components/AgentAvatar.tsx`
- [x] 4.3 Extend `frontend/src/components/ui/__test__/Avatar.test.tsx` and `frontend/src/components/__test__/AgentAvatar.test.tsx` to cover both paths — `live` passes the fork props through and drops the clip; its absence renders the same element as today

## 5. Hook and wiring (D4, D5, D6, D7, D8)

- [x] 5.1 Create `frontend/src/features/chat/run/useLiveAvatar.ts`: read `entries`, `activeTurnID`, `dispatching`, and `lastTurnOutcome` via `useChatStore`, run `deriveAvatarState`, own the fork ref, and react to `lastTurnOutcome` identity changes — completed → nod + sparkle ≈2 s, cancelled → shake + sweat ≈2 s, error → shake — with a seen-ref initialised on mount so no reaction replays on mount, remount, or session switch
- [x] 5.2 In the same hook, call `wake()` when `dispatching` turns true or `activeTurnID` goes null → value (D5), pass `lookAt: "global"` and `sleepAfter ≈ 120000` (D6), and return `undefined` under `useReducedMotion()` (`frontend/src/features/chat/tools/useReducedMotion.ts`, D7)
- [x] 5.3 In `frontend/src/features/chat/components/RightBar.tsx`, render the identity branch's avatar as `<AgentAvatar … live={live} />` (D8)
- [x] 5.4 Create `frontend/src/features/chat/run/__test__/useLiveAvatar.test.tsx` using fake timers to assert nod/shake on outcome transitions, no call on mount, wake on turn start, and `undefined` under reduced motion; verify `frontend/src/features/chat/__test__/RightBar.test.tsx` keeps passing

## 6. Integration checks

- [x] 6.1 `cd frontend && npx tsc --noEmit -p .`, `vp check src`, and `npx vitest run` all pass
- [x] 6.2 `task build` exits 0 last, so bindings are not stripped

## 7. Live smoke in the desktop app

- [x] 7.1 Via `task build` + `task run`: run a turn that reasons, runs a tool, and streams text to completion — the face moves through thinking → tool call → speaking and nods with a sparkle on completion
- [x] 7.2 Hold an approval open (listening + question badge), cancel a turn (shake + sweat drop), and force an error followed by a reply (the error face holds until the next user message)
- [ ] 7.3 Open and close a preview mid-turn (the reaction is dropped, not deferred), switch sessions, toggle system reduced motion (a still picture), and check both themes — 2026-10-26 headless smoke (server mode + in-app browser): preview/knowledge swap unmounts the face and the back action remounts it at `idle`, session switch follows the new transcript, and both themes render; the system reduced-motion toggle was not exercised (no system setting in the headless run; covered by the hook's unit test) — run it once on the desktop app
