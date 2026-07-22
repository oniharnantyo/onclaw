## Context

- Eino's `TypedChatModelAgentMiddleware.AfterAgent` is documented to run only on successful terminal states (final answer, return-directly tool result) and is explicitly NOT called on `ErrExceedMaxIterations`, context cancellation, or model errors. The interface offers no finalize hook (`OnError`/`OnEnd`/`Finally`). Confirmed against the eino adk source: error/interrupt paths route to `handleRunFuncError`, which emits a terminal `Err`/interrupt event and never calls `AfterAgent`.
- On error/interrupt the iterator yields an `Err`/interrupt event then closes; on cancel the iterator's `ctx.Err()` short-circuits at the top of `Next()`. All three are reachable as terminal `Event{}, false` in `event_iterator.go`.
- `HistoryMiddleware.AfterAgent` already uses `context.WithoutCancel(ctx)` + a 5s timeout to persist despite stream-teardown cancellation — but only when it is actually called. `TestHistoryMiddleware_PersistsDespiteCancelledContext` validates that path.
- The iterator and the history middleware hold **separate** message accumulations: `eventIterator.collectedMsgs` feeds the memory middleware via `onStopFlush`; `HistoryMiddleware.bufferedMessages` (fed by `AfterModelRewriteState`) feeds `AppendTurn`. A fix cannot just flush `collectedMsgs` — it must trigger the middleware's own persistence.

## Goals / Non-Goals

**Goals**

- A `conversation_messages` row is written for every turn, including those that error / are interrupted / are cancelled.
- The frontend can distinguish interrupted / cancelled / error / clean termination.
- No framework event is dropped without a log line.

**Non-Goals**

- Incremental (per-message) persistence — stays a single batch write at terminal.
- HITL resume semantics / interrupt payload surfacing.
- New `Event` variants for unused Eino control-flow actions.

## Decisions

### 1. Trigger persistence from the iterator, since Eino offers no finalize hook

With no `OnError`/`Finally` available, the event iterator fires an idempotent `onTurnPersist` callback on its three abnormal terminal branches. The callback calls `HistoryMiddleware.PersistTurn(ctx)`, which reuses the existing `persistTurn` logic and its detached-write context.

### 2. Fire-once on abnormal branches only; normal completion stays on Eino's `AfterAgent`

On normal completion Eino invokes `AfterAgent` inside the chain before the generator closes, so persistence is already done when the iterator later reads `!ok`. Routing normal completion through the new callback too would do redundant work and race with Eino's own `AfterAgent`. `persistTurn`'s buffer-clear is a safety-net idempotency guard, not the primary control.

### 3. Idempotency via buffer-clear

`persistTurn` early-returns when `bufferedMessages` is empty and clears the buffer at the end, so a second invocation is a no-op. The iterator also nils the callback after first fire. Double-persist is impossible.

### 4. Lock `bufferedMessages` for the new cross-goroutine access

`persistTurn` now runs on the iterator goroutine while the chain may still mutate `bufferedMessages` on the cancel path. Both `accumulateNewMessages` and `persistTurn` take the existing `h.lock`.

### 5. Terminal reasons via `it.Err()` sentinels, not new `Event` variants

Consistent with the existing cancel → `context.Canceled` flow; consumers already call `it.Err()` post-loop. The SSE layer becomes a pure translator. A new `Event{Interrupted}` variant would split terminal signaling across two channels.

### 6. `done` already means finished

Clean completion already emits `turn`/`usage`/`done`; no new `finished` SSE event is needed.

## Risks / Trade-offs

- **Partial-turn content on cancel**: a cancelled/interrupted turn is persisted with whatever `AfterModelRewriteState` accumulated (response ID / tokens may be absent → graceful empty fallback). Intended (the user wants the partial history), but such rows can have empty `response_id` and zero tokens — acceptable and already degraded gracefully.
- **Cross-goroutine mutation**: mitigated by `h.lock`; the cancel path is the only new concurrent case.
- **SSE behavior change**: clients that treated any terminal as `error` now may receive `cancelled`/`interrupted`; the Web UI is updated in lockstep. External SSE clients see new (additive) event names.
