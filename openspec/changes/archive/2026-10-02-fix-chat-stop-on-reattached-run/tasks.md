# Tasks

## 1. Catch-up stream abort handle (livechat.ts)

- [x] 1.1 In `web/src/lib/livechat.ts`, give `attachCatchUpStream` its own `AbortController` registered in a module-level registry keyed by `chatId` (last-writer-wins: a new registration aborts the previous entry for the same chat), chain it to the caller-passed signal, and export `abortCatchUpStream(chatId)`; verify with a unit test that registering a second attach for a chat aborts the first and that the caller-signal abort still suppresses `onDone`/`onError`
- [x] 1.2 Guard `attachCatchUpStream`'s `onEvent` (and `write`) on the signal being live: an event arriving after abort must not assert `running: true` and must not write to the store — extend the livechat unit test to fire a stale event post-abort and assert the store's `ui.running` stays false

## 2. Cancel addressing fallback (runtime.tsx)

- [x] 2.1 In `onCancel` (`web/src/chat/runtime.tsx`), abort the followed stream via `abortCatchUpStream(chatId)` before `patchUi({ running: false })`; when `inFlight` holds identity keep current behavior byte-identical, otherwise resolve agent slug from the store's agent lookup and the session from `activeBoundSessionId(tenantId, chatId)` and call `api.agents.cancelRun(slug, boundSession, 'pending')` — unit test both branches in `runtime.test.ts` (fresh turn keeps existing assertions; empty-`inFlight` turn fires cancelRun with `pending`)
- [x] 2.2 Regression-test the conflict-queue interplay in `runtime.test.ts`: send into a 409-conflict session, press stop during the catch-up attach, and assert no redispatch of the queued send, the user row remains in the transcript, `ui.running` stays false, and no "Lost the live stream" toast fires from the stop-abort

## 3. Turn-tail preservation (D5 root-cause)

- [x] 3.1 Write the failing store-level test first: stop mid-catch-up, let the run reach its terminal state, assert every streamed part (tool cards, reasoning, text) remains rendered in the live transcript; root-cause the vanish (translator seed-tail/owned-set across the terminal event vs. hydration re-run replacing `sess.messages`) and fix it — test green
- [x] 3.2 Add a composer-level assertion that the stop control clears immediately on click and does not reappear from subsequent followed-stream events (`Composer.test.tsx` or `ChatView` suite)

## 4. Chat header running badge conformance

- [x] 4.1 Derive the conversation header's running state from the merged signal the sidebar session indicator uses (instant local run state preferred, server session-list running flag second) so the header reads "Running · last active …" during a live run; cover with a component test asserting the header flips to Running while `ui.running` is true for the active session and back to Idle on terminal state

## 5. Integration verification

- [x] 5.1 Run the web suite (`pnpm test` in `web/`) and `go build ./... && go test ./...` to confirm no backend drift; fix anything surfaced
- [ ] 5.2 Live pass against the dev instance: send a long web-search turn, reload mid-run, press Stop on the re-attached run, and verify — cancel endpoint reached (run stops server-side), composer settles on Send, sidebar "Run in progress" clears, header shows Running while streaming and Idle after stop, turn tail intact without reload; repeat once with the second-tab conflict-queue path
