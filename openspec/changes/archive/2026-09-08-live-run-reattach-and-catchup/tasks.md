## 1. Synchronous Session Persistence on Message Send

- [x] 1.1 Update `web/src/store/index.ts` to execute an immediate synchronous `persistAllThreads` write in `ensureSessionBinding` and `pushMsg`.
- [x] 1.2 Add frontend unit tests in `web/src/store/threadPersistence.test.ts` verifying that `sess_<uuid>` bindings are written to localStorage synchronously before any debounced timer fires.

## 2. Multi-Subscriber Live Broadcaster in Runner

- [x] 2.1 Refactor `liveRun` and `runManager` in `internal/agents/runmanager.go` to support dynamic subscriber streams (`Subscribe`, `Unsubscribe`, and `Broadcast`).
- [x] 2.2 Update `Runner.streamRun` and `Runner.drainAgentEvents` in `internal/agents/runner.go` to broadcast events to all active subscribers on the `runHandle`.
- [x] 2.3 Add unit tests in `internal/agents/runmanager_test.go` verifying that multiple concurrent subscribers receive the live stream and that slow/closed subscribers do not stall the run.

## 3. Streaming Session Events Endpoint with Replay & Live Switchover

- [x] 3.1 Update `GET /workspaces/:ws/agents/:agent/sessions/:session/events` handler in `internal/server/handlers/agents.go` to support `stream=true` and `after=<cursor>` query parameters with SSE (`text/event-stream`).
- [x] 3.2 Implement two-phase streaming in `agents.go`: query and stream historical events from `session_events` where `seq > after`, seamlessly attach to the active `runManager` live subscriber, deduplicate overlapping events, and stream to terminal completion / `[DONE]`.
- [x] 3.3 Add backend integration tests in `internal/server/agents_test.go` verifying streaming catch-up on completed sessions, active runs, and disconnect handling.

## 4. Frontend In-Flight Re-attachment & Catch-up Stream

- [x] 4.1 Implement `streamSessionEvents` in `web/src/lib/livechat.ts` to consume the SSE session-events endpoint and update the thread in real time.
- [x] 4.2 Update `ChatRouteActive` in `web/src/screens/ChatRoute.tsx` and `useChatRuntime` in `web/src/chat/runtime.tsx` to detect unfinished turns on mount / hydration, connect to the streaming session events endpoint, and stream incoming deltas into the active assistant message.
- [x] 4.3 Add frontend tests in `web/src/lib/livechat.test.ts` and `web/src/screens/ChatRoute.test.tsx` verifying transcript catch-up and live stream re-attachment.

## 5. End-to-End Verification

- [x] 5.1 Run full backend verification (`go test ./...` and `go vet ./...`).
- [x] 5.2 Run full frontend test suite (`pnpm test` and `pnpm build`).
- [ ] 5.3 Verify manually in browser: send a multi-second prompt, refresh the browser at 1s, verify the reloaded page catches up and streams the rest of the response to completion with correct tool cards and context meter.

## 6. Fix Pass: Reattach Reliability (2026-09-08)

Manual pass 5.3 found two failures: a page reloaded mid-run showed the frozen
partial transcript (never streamed the remainder), and a send during the
still-active run surfaced the 409 run-lock conflict as a "Run failed" card.

- [x] 6.1 ChatRoute attach guard survives StrictMode: the sticky
  `hydratedRef` made the remounted effect bail while its first attempt was
  aborted — the catch-up stream never attached after a reload. The guard now
  lives on a ref cleared in cleanup, and `hydrateSession` takes an abort
  signal so a stale attempt can't clobber a newer attach's streamed tail.
  Regression test renders under `<StrictMode>`.
- [x] 6.2 `run_active` status frame: the streaming session-events endpoint
  writes a synthetic `run_active` transcript event ahead of the history
  replay whenever its tap attached, so the reloaded page flips to the running
  state immediately (the first real event can be seconds away mid-tool-call).
  Client: the translator ignores it; the generic onEvent path flips
  `ui.running`.
- [x] 6.3 409 conflict queue: `runTurn` flags `meta.conflict` (HTTP 409 from
  the per-session run lock); the chat runtime retracts the empty optimistic
  row, attaches the catch-up stream so the active turn streams to completion
  in-thread, and re-dispatches the queued turn once (chained via the drained
  run's response id) when the stream ends — a second conflict surfaces as a
  real failure instead of looping.
- [x] 6.4 Reattach write hardening: catch-up writes replace owned messages in
  place (id-keyed) instead of re-appending (an owned tail never jumps below
  messages typed since), the tail seed scans backward for the newest agent
  message with a session response id, and a duplicate `tool_call_started`
  (replay + tap boundary overlap) never duplicates its card.
