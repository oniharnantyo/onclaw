# Proposal: enhance-resumable-streams

## Why

Re-entering a session mid-turn does not resume the stream where the user left it: the text streamed before re-entry is missing and the message continues mid-call. The root cause is an ordering race — the chat client attaches its event listener only after the backend `Subscribe` call resolves (`client.ts`), while the backend's subscribe-time catch-up snapshot (design D5) is queued and pumped to the event emitter before the RPC returns, so the snapshot is dispatched with no listener attached and is dropped (`@wailsio/runtime` dispatches to currently-registered listeners only; the transcript cannot cover for it because the in-flight call's partial text is live-only by design). The same push-only fragility leaves a second hole: a transport blip (server-mode WebSocket reconnect) loses deltas silently for a subscription that stays registered, and nothing ever re-fires the snapshot.

The assistant-ui resumable-streams guides define the target contract — the server persists in-flight stream state, any reattaching consumer replays state-then-live, and a reconnect triggers resume. buddies already implements most of this contract natively (in-memory producer that outlives any consumer, subscribe-time snapshot, turn-state query); this change closes the gap between the contract and the current behavior.

## What Changes

- **Subscribe ordering fix**: the chat client attaches the session event listener before calling the backend `Subscribe`, so the catch-up snapshot can no longer be dropped in the attachment window; events arriving during a session load keep landing in the existing page-load buffer.
- **Catch-up snapshot completeness**: the in-flight state delivered to a reattaching consumer grows from "text partial + running tools" to also cover the current call's reasoning partial and the currently running delegated-task rows, so reconstruction matches what a subscriber who never left would see.
- **Stream liveness beacon**: while a turn streams, the runtime emits a periodic live-only beacon carrying the turn id and a monotonic count of deltas emitted for that turn, so a consumer can prove the pipe is alive and detect missed deltas exactly instead of guessing from silence.
- **Resync verb**: a new bound call asks the runtime to re-deliver the full in-flight state of a busy session (unique synthetic id per emission, live-only, never persisted); consumers request it when the beacon reports a gap or staleness, or on window focus of a busy session.
- **Replace-fold in the chat view**: the transcript fold learns to reconcile a resync snapshot against the streaming entry — replacing the partial text rather than appending — so resume is exact rather than cumulative.

Notably out of scope: groups (their lane is durable-first — persisted rows replayed by refetch — with nothing live-only to lose), delta persistence (the session log keeps its "replay never depends on delta granularity" rule), adopting the `assistant-stream` package (welded to the AI-SDK HTTP transport buddies does not use), and a startup sweep for turns killed by an application restart mid-turn (a separate producer-lifecycle change).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the subscribe-time catch-up requirement is widened — any reattaching consumer (fresh subscribe or explicit resync) receives the full in-flight state including reasoning and running tasks, and a streaming turn emits a liveness beacon with a delta counter.
- `chat-view`: the session re-entry requirement grows a replace-fold for resync state, the client trigger rules (beacon gap/staleness, focus), and the listener-before-subscribe ordering that makes reconstruction reliable.

## Impact

- **Go backend**: `internal/agents` (running-turn state: reasoning partial + running-task tracking; `Subscribe` unchanged; new `Resync`; beacon ticker per streaming turn), `internal/agents/subagents` (running-task snapshot accessor), `internal/sessions` (new live-only event kinds for the beacon and the resync snapshot), `internal/services/chat.go` (bound `Resync`).
- **Frontend**: `frontend/src/lib/client.ts` (listener-before-subscribe ordering; `resync` method), `frontend/src/features/chat/store.ts` (resync trigger wiring, beacon handling), `frontend/src/features/chat/entries.ts` (replace-fold).
- **Both hosts by construction**: the resync and beacon are transport-agnostic; server mode inherits them through the same service surface.
- **No breaking changes**: all new events are live-only kinds; the durable event vocabulary and the persisted transcript format are untouched; existing bindings gain additive methods only.
