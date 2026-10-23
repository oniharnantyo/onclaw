# Design

## Context

The backend turn is fully decoupled from subscribers: events persist unconditionally, live fan-out (`broadcast`) is a best-effort tap, and `Cancel` is a two-level safe-point/hard escalation. The frontend, however, owns its turn lifecycle purely from its own dispatch path (`activeTurnID`, `busy`) plus live turn-activity events, and rebuilds session state on every `selectSession` from the durable transcript page. Three consequences (see proposal.md — Why): re-entry reports idle while the backend is busy; in-flight live-only state (text deltas, running-tool announcements) is unrecoverable on re-entry; and the teardown-before-resubscribe sequence can delete the freshly created backend subscription because the two Wails calls are handled concurrently and may interleave.

The uncommitted `adopt-agentic-runtime` work is in flight on this working tree; this change layers on top of it and must not restructure the pieces it touches (runner drain, subscription registration) beyond the additions specified here.

## Goals / Non-Goals

**Goals:**

- Re-entering a busy session shows the true turn state: stop control, working row, correct send/queue behavior, and a clean settle on the terminal event.
- Re-entered transcripts reconstruct the in-flight partial text and running tool calls at subscribe time, without duplicating durable content.
- The session opened by a switch always keeps a working live feed.

**Non-Goals:**

- Persisting deltas or any event-classification change (live-only deltas stay live-only; write-amplification trade-off unchanged).
- A stop control on session-rail rows for background sessions (follow-up UX; the rail indicator already works).
- Changing cancellation semantics or escalation timing (`cancelEscalationDelay` stays 2s).
- Reconstructing in-flight *reasoning* partials (only text partials and tool calls are tracked today; the reasoning disclosure appears when its durable event lands).
- Fixing the dispatch-window cancel race (a cancel clicked while `Send` is still resolving can hit the backend before the running record exists; the spec already classifies it as a safe no-op, and a second click works).

## Decisions

- **D1 — Turn-state query returns the turn id, not just busy.** New `Manager.TurnState(ctx, sessionID) (busy bool, turnID string)` reading `m.running[sessionID]` (parked approvals included — the record survives parking), exposed as `ChatService.TurnState` and one generated binding. The id matters: the store's terminal guard (`turnID !== s.activeTurnID → ignore`) needs the real id, so re-arming with the genuine id lets the existing `turn_completed/cancelled/error` handling settle state without a special case. Alternative considered: deriving busy from `runningSessions` alone — rejected; it is fed by live events only, so an app restart or a never-subscribed session (scheduler-triggered) stays blind.

- **D2 — Re-adopt runs after the transcript page lands, and converges from both race directions.** `selectSession` calls `TurnState` after the page fold: busy → set `activeTurnID` to the returned id and mark the session running. If the terminal event arrived during the fetch, it folded with `activeTurnID` null (the guard passes) and `TurnState` now reports idle — nothing to adopt. If it arrives after the query, the adopted id matches and `completeTurn` proceeds normally. No new state field: adopting `activeTurnID` makes `setBusy()` true, which the composer's stop control and queue row already key off.

- **D3 — Subscription teardown is split from subscription replacement.** `client.chat.subscribe`'s cleanup becomes local-only (remove the Wails event handler); a new explicit `client.chat.unsubscribe(sessionID)` performs the backend call. `selectSession` stops unsubscribing the target before re-subscribing — the backend's Subscribe already closes the replaced subscription — so the concurrent-call interleave that could delete the new subscription is removed structurally rather than by relying on call ordering. The teardown paths that genuinely end a session's subscription (`selectAgent` pruning, `deleteSession`) call the explicit unsubscribe. Belt-and-suspenders alternative (generation tokens on Subscribe/Unsubscribe) rejected as unnecessary once the frontend stops firing the destructive call.

- **D4 — Live events arriving during the initial page fetch are buffered, then folded.** Today any event ingested between subscribe and `set({ entries })` is discarded by the page's state overwrite (and live-only events are not in the page to recover). `selectSession` buffers live events for the loading session and folds them after the page lands — page first, then buffer, dedupe by event id as today — so the snapshot (D5) and any deltas that raced the fetch survive. A superseded switch drops the buffer with its `loadSeq` guard.

- **D5 — The catch-up snapshot is emitted by the manager at subscribe, from memory, as ordinary live-only events.** `runningTurn` gains an in-flight tool map (`callID → {name, args}`) maintained by the drain's record tap: added on the live running announcement, removed on the terminal record, guarded by a small mutex alongside the existing `partial` accumulator. `Manager.Subscribe` — while still holding `m.mu`, so no broadcast can interleave — enqueues onto the new subscription: one `KindTextDelta` carrying the accumulated partial (the reducer folds it as the start of the streaming entry, since no partial entry exists in a fresh page) and one running `KindToolActivity` per in-flight call. Events are minted with stable synthetic ids (dedupe-safe), marked so they are never persisted (direct channel send, not `record`). Emitting native event kinds reuses the entire existing fold/render path; a dedicated snapshot event kind would need new reducer and renderer handling for no benefit.

- **D6 — Session-owned event ids keep dedupe correct across the snapshot.** Synthetic snapshot ids derive from the turn id and a fixed suffix (`snap-<turnid>-partial`, `snap-<turnid>-tool-<callid>`), so a duplicate subscribe (switch away and back quickly) re-mints the same ids and `seenEventIDs` — reset per session open — cannot double-fold them within one view.

## Risks / Trade-offs

- [Snapshot races a terminal (turn completes between the busy check and the send)] → The snapshot is enqueued under `m.mu`; `finishTurn` deletes the record under the same lock, so the snapshot is either complete (terminal is durable and in the fetched page — folding converges) or absent (no turn — no snapshot). No torn state.
- [Re-adopt could resurrect a just-finished turn if `TurnState` answered before a terminal that the frontend has not folded yet] → The terminal event is ordered after subscribe on the same session channel; folding it runs `completeTurn` with the adopted id. Worst case the stop control shows for the sub-second window before the terminal lands.
- [Live-only reconstruction is best-effort by design] → The spec pins reconstruction of text partials and running tools; reasoning partials in flight are not reconstructed and appear when their durable event lands (Non-Goal).
- [Bindings regen mid-change breaks parallel frontend tsc] → Generate bindings once (`task common:generate:bindings --force`) in the wave that adds the service method, before frontend tasks consume them.

## Migration Plan

No schema or persisted-format changes; nothing to migrate. Rollback is a plain revert — the new binding is additive and unused by older callers. Ship order inside the change: backend (manager + service + bindings) before the frontend store work that consumes it.

## Open Questions

(none)
