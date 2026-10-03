# Design

## Context

The cancel contract for live turns runs through one module-level `inFlight` record in `web/src/chat/runtime.tsx:87` (`responseId`, `agentSlug`, `sessionId`, `abort`, …). Both `startTurn` and the regenerate path populate it, and `onCancel` (runtime.tsx:676) reads everything from it: it aborts the stream via `inFlight.abort` and calls `api.agents.cancelRun` only when `inFlight.agentSlug && sessionId` resolve. Runs the client did not start in this view are followed through `attachCatchUpStream` (`web/src/lib/livechat.ts:740`) — from ChatRoute's hydration effect after a mid-run reload, or from the 409-conflict path in `respondFor` (which calls `clearInFlight()` before attaching). That path never touches `inFlight`, so stop finds `abort === undefined` (no local detach) and `agentSlug === undefined` (no server cancel), while `attachCatchUpStream`'s per-event `patchUi({ running: true })` immediately undoes the `running: false` at the top of `onCancel`.

Backend cancel is healthy: `POST /workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel` is session-scoped, `Runner.CancelRun` unwinds at the next safe point, and the codebase already uses the session-scoped addressing with a `pending` turn placeholder for stop-before-first-event (`sessionIdFromResponseId` fallback in `onCancel`).

## Goals / Non-Goals

**Goals:**
- Stop detaches the followed stream and reaches the server cancel endpoint for any followed run, with or without a captured response identity.
- Running state stays cleared after stop (no re-assertion from stream events).
- Turn tail survives stop in the live transcript; conflict-queued send is held, not redispatched.
- Chat header run badge matches the existing `web-app/agents` "Running · last active …" requirement.

**Non-Goals:**
- No backend changes (`agent_runs.go`, `Runner.CancelRun` verified working).
- No change to the sidebar session running indicator (already spec-conformant and truthful).
- No new stop surfaces for channels, schedulers, or gateways — agent chats only, matching today's control.
- No rework of the conflict-queue flow itself beyond stop's interaction with it.

## Decisions

### D1 — Two owned halves instead of repopulating `inFlight` from the catch-up path

**Chosen:** keep the two lifecycles separate and give each half an owner where its state already lives.

1. *Stream detach*: `attachCatchUpStream` creates its own `AbortController`, registers it in a module-level registry in livechat.ts keyed by `chatId` (last-writer-wins: registering aborts any previous entry for the same chat), and exports `abortCatchUpStream(chatId)`. The caller-passed signal (chat switch/unmount) stays authoritative: the registered controller aborts when the caller's signal aborts, so `onDone`/`onError` suppression semantics ("neither fires when the signal aborts") are unchanged.
2. *Server cancel*: `onCancel` already closes over `tenantId` and `chatId` (`useChatRuntime(chatId)`). When `inFlight` holds identity, current behavior is preserved verbatim. When it doesn't, resolve the agent slug from the store's agent lookup (same lookup `respondFor` uses) and the session from `activeBoundSessionId(tenantId, chatId)`, and call `cancelRun(slug, boundSession, 'pending')`. The existing `.catch(() => {})` already tolerates "nothing live".

**Alternative rejected:** make `attachCatchUpStream` write into `inFlight`. That requires moving `inFlight` to a shared module (import cycle: runtime.tsx imports livechat.ts) and overloads a single-slot *turn* record with a *stream* lifecycle that outlives turns — `clearInFlight()` at turn boundaries would silently drop the stream's abort handle and reintroduce the bug from the other side.

### D2 — Stop must win the race against in-flight stream events

`onCancel` calls `abortCatchUpStream(chatId)` **before** `patchUi({ running: false })`. Additionally, `attachCatchUpStream`'s `onEvent` checks `signal.aborted` before asserting `running: true` or writing, so an event already in the macrotask queue when stop fires cannot flip the composer back. Verify `streamSessionEvents` stops delivery on abort; if its error path treats an abort as an error, the abort listener must suppress the "Lost the live stream" toast (stop is not a failure).

### D3 — Conflict-queued send held on stop, by construction

In the conflict path the queued redispatch hangs off the catch-up's `onDone`. Stop aborting the stream means `onDone` never fires, so the send is held without new dispatch logic. `dispatchQueuedHead` in `onCancel` keys on `inFlight.cid`, which is empty on the followed path — the guard already prevents an accidental dispatch. The optimistic user row stays in the transcript (the conflict path never retracts user messages). Regression-test the whole interplay, since it spans three files.

### D4 — Header badge sources the same merged signal as the session indicator

The conversation header currently derives its status from a slower agent-level field, so it reads "Idle" while a run streams. Derive the running state for the header from the same two sources the sidebar session indicator merges (the app's own run state for the active session — instant — and the server session list's running flag), preferring the instant local signal. This is conformance with the existing `web-app/agents` requirement, not new behavior; no spec delta.

### D5 — Vanished turn tail: root-cause first, then fix

The live transcript lost the whole turn row after a followed run completed (post-stop); a reload restored it from server history. `retractIfEmpty` no-ops with empty `inFlight` and the catch-up `write()` replaces/appends but never deletes, so the mechanism is not obvious from static reading. Approach: reproduce with a failing store-level test (stop mid-catch-up → run completes → assert the tail remains), root-cause from there, then fix. Most likely suspects: the transcript translator's seed-tail/owned-set bookkeeping across the terminal event, or a hydration re-run replacing `sess.messages` mid-stream.

## Risks / Trade-offs

- [Double cancel addressing] Fresh-turn stop now has two conceivable addressings → keep the `inFlight`-first gate; the fallback fires only when identity is missing, so the fresh path is byte-identical to today.
- [Registry leak on StrictMode double-attach] ChatRoute's `catchUpRef` guard already ensures one live attach per session key; last-writer-wins registration additionally aborts any stale controller, so a leaked entry cannot keep a dead stream "current".
- [Stop on a foreign-tab run] Session-scoped cancel stops the run for every viewer of that session — already true of the endpoint and the spec's session model; no new semantics.
- [`pending` turn placeholder] Already the documented addressing for stop-before-first-event; no server change, but the smoke/live pass must cover the followed-run path end-to-end since unit fakes won't exercise the real 409/re-attach ladder.

## Migration Plan

Web-only change: build and deploy with the next frontend wave; no data, schema, or API migration. Rollback is a plain revert.

## Open Questions

None blocking.
