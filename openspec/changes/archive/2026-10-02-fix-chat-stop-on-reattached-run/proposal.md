# Proposal

## Why

Pressing "Stop generating" in an agent chat does nothing when the page is following a run it did not start in the current view — after a mid-run reload (or when a second tab / double-send lands in the 409 conflict queue), the stop control is visible but neither aborts the followed stream nor reaches the server cancel endpoint, so the run streams to full completion while the user keeps clicking a dead button. Verified live against the dev instance on 2026-09-30: composer stuck on Stop for 30s+, sidebar stuck on "Run in progress", reload afterwards showed the full un-stopped answer.

## What Changes

- Stop on a followed run (one the client re-attached to via the catch-up stream) must actually cancel: abort the local catch-up stream AND call the session-scoped cancel endpoint addressed by the bound session, regardless of whether the client holds the turn's minted response identity.
- The running state must stay cleared after stop: the followed stream's subsequent events must not re-assert the composer spinner.
- Stopping a followed run must preserve the turn tail in the live transcript (no vanishing turn row) and must not auto-dispatch a conflict-queued send whose catch-up stream was aborted by the stop.
- Chat header run badge conformance: during a live run the conversation header must read "Running · last active …" — the existing `web-app/agents` requirement already mandates this; today the header shows "Idle" while the run streams. Implementation-only, no spec delta.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/chat-runtime`: the "Live cancel" requirement changes — cancel addressing no longer depends on a captured response identity. Stop must work on runs the client is merely following (reload re-attach, conflict-queue attach), must detach the followed stream so the running state stays cleared, must keep the streamed turn tail rendered in the live transcript, and must not redispatch a conflict-queued send whose catch-up the stop aborted.

## Impact

- `web/src/chat/runtime.tsx` — `onCancel` (session-scoped fallback addressing, abort of the followed stream), the module-level `inFlight` record, and the 409-conflict path that wipes it.
- `web/src/lib/livechat.ts` — `attachCatchUpStream` must expose an abort handle (registry keyed by chat) so the stop control can detach the followed stream.
- `web/src/screens/ChatRoute.tsx` — hydration effect's catch-up attach wiring (owns the current AbortController).
- Chat header status derivation (`ChatView` header) — source run state from the same merged signal the sidebar session indicator uses.
- No backend change: `Runner.CancelRun` and `POST /workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel` were verified live — session-scoped, accepts the `pending` turn placeholder, returns cleanly when no run is live.
- Tests: `web/src/chat/runtime.test.ts`, composer/chat view suites, plus a live pass of the reload-mid-run stop flow.
