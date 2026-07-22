## Why

When an agent turn ends abnormally — on model/agent error, an interrupt, or context cancellation (user clicks Stop) — the conversation history for that turn is silently lost. History is persisted in exactly one place: `HistoryMiddleware.AfterAgent` → `ConversationStore.AppendTurn`. Eino's `TypedChatModelAgentMiddleware` interface documents that `AfterAgent` runs **only on successful terminal states** and is *not* called on error/interrupt/context-cancellation; the interface provides no `OnError`/`OnEnd`/`Finally` hook. The event iterator's abnormal branches return terminal without persisting, so `bufferedMessages` (the partial turn, already accumulated by `AfterModelRewriteState`) is discarded — no row is written to `conversation_messages`.

A second, related problem: the iterator collapses every abnormal terminal into one indistinguishable signal. The interrupt branch leaves `it.err == nil`, so callers cannot tell interrupt from clean finish; context cancellation is stored as `context.Canceled` but the API SSE layer reports every non-clean terminal as a generic `error`. The React UI cannot render "Interrupted" or "Stopped by user" distinctly from a real error.

Finally, several Eino event kinds (`Action.Exit`, non-summarization `CustomizedAction`, empty `MessageOutput`, `Output.CustomizedOutput`, `SessionEventVariant`) fall through the iterator loop with no trace, making "why did the agent skip X" impossible to diagnose.

## What Changes

- **Persist the partial turn on abnormal terminal**: extract `HistoryMiddleware`'s persistence into an idempotent `persistTurn`; expose `PersistTurn(ctx)`; fire it (once) from the event iterator's error / interrupt / cancel branches so `conversation_messages` always receives the turn. Normal completion keeps using Eino's `AfterAgent` to avoid a double-persist race.
- **Distinguish terminal reasons via `it.Err()`**: add an `ErrInterrupted` sentinel so `it.Err()` cleanly reports interrupted / cancelled / error / clean; the interrupt branch sets it instead of leaving `err == nil`.
- **Emit distinct SSE lifecycle events**: the API translates terminal states into `interrupted`, `cancelled`, and `error` SSE events (clean finish keeps `done`); the CLI prints `[Interrupted]` in parity with its existing `[Turn Canceled]`.
- **Surface lifecycle events in the Web UI**: `runChatStream` + `ChatProvider` handle `interrupted`/`cancelled`, reusing the existing `stopped` flag to mark the partial assistant turn.
- **Observe silently-dropped events**: add `slog.Debug`/`Warn` for each fall-through branch and guard empty `MessageOutput`, so no event vanishes without a trace.

## Capabilities

### Modified Capabilities

- `conversation-history`: SHALL persist the (partial) turn on error, interrupt, and context-cancellation terminal states (cross-ref `agent-core`).
- `agent-core`: SHALL expose the terminal reason via `EventIterator.Err()` using typed sentinels; SHALL log every framework event it does not surface.
- `chat-ui`: the SSE stream SHALL emit distinct terminal events (`interrupted`, `cancelled`, `error`); the Web UI SHALL render those terminal states distinctly.

## Impact

**Affected code:**
- `internal/agent/middlewares/history_middleware.go` — extract `persistTurn`, add `PersistTurn`, lock `bufferedMessages`.
- `internal/agent/agent.go` — wire `onTurnPersist` callback; pass to iterator.
- `internal/agent/event_iterator.go` — fire-once `onTurnPersist` on abnormal terminals; set `ErrInterrupted`; silent-drop logs; empty-`MessageOutput` guard.
- `internal/agent/event.go` — `ErrInterrupted` sentinel.
- `internal/api/handler/chat.go` — SSE terminal dispatch.
- `internal/cli/chat.go` — interrupt parity.
- `web/src/types/chat.ts`, `web/src/components/chat/runChatStream.ts`, `web/src/components/ChatProvider.tsx` — `interrupted`/`cancelled` handling.

**Affected systems:** agent runtime, conversation store, API SSE, Web UI.

**Dependencies:** none new.

**Non-goals:** new mid-stream `Event` variants for Eino features onclaw doesn't use (`Exit`, `TransferToAgent`, `BreakLoop`, `CustomizedOutput`, `SessionEventVariant`) — logged only; HITL resume with `CheckPointID`/`InterruptContexts` payload; panic-path persistence via `defer`; incremental per-message persistence.
