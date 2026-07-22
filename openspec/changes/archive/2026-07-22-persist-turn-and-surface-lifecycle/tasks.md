# Implementation Tasks

## 1. History persistence on abnormal terminal

- [ ] 1.1 `internal/agent/middlewares/history_middleware.go`: extract `persistTurn(ctx) error` from `AfterAgent` (current lines 188–312); `AfterAgent` calls `accumulateNewMessages(state.Messages)` then `persistTurn`.
- [ ] 1.2 Add exported `PersistTurn(ctx context.Context) error` (takes `h.lock`) that calls `persistTurn`.
- [ ] 1.3 Guard `bufferedMessages`: `accumulateNewMessages` and `persistTurn` both take `h.lock`.
- [ ] 1.4 `internal/agent/agent.go`: add `onTurnPersist` closure calling `historyMiddleware.PersistTurn(ctx)` (guard nil); pass to `eventIterator`.
- [ ] 1.5 `internal/agent/event_iterator.go`: add `onTurnPersist func()` field; fire-once (nil after first call) on ctx-cancel (30–34), `event.Err` (62–68), `Action.Interrupted` (72–75); do NOT fire on normal `!ok`.
- [ ] 1.6 Test `history_middleware_test.go`: `PersistTurn` persists a buffered partial turn; a second call is a no-op.
- [ ] 1.7 Test `event_iterator_test.go`: each abnormal terminal fires `onTurnPersist` exactly once.
- [ ] 1.8 Integration test: an errored/cancelled turn writes exactly one `conversation_messages` row.

## 2. Terminal-reason surfacing (backend)

- [ ] 2.1 `internal/agent/event.go`: add `var ErrInterrupted = errors.New("agent interrupted")`.
- [ ] 2.2 `internal/agent/event_iterator.go`: interrupt branch sets `it.err = ErrInterrupted` before returning terminal.
- [ ] 2.3 `internal/api/handler/chat.go`: post-loop dispatch via `errors.Is` → `cancelled` / `interrupted` / `error`.
- [ ] 2.4 `internal/cli/chat.go`: extend the `context.Canceled` block with `errors.Is(err, agent.ErrInterrupted)` → `[Interrupted]`.
- [ ] 2.5 Tests: `event_iterator_test.go` asserts `errors.Is(it.Err(), agent.ErrInterrupted)`; `chat_test.go` asserts the correct SSE event per branch.

## 3. Web UI lifecycle handling

- [ ] 3.1 `web/src/types/chat.ts`: add `SSEInterruptedEvent`, `SSECancelledEvent`.
- [ ] 3.2 `web/src/components/chat/runChatStream.ts`: add `onInterrupted`/`onCancelled` callbacks + dispatch for event names `interrupted`/`cancelled`.
- [ ] 3.3 `web/src/components/ChatProvider.tsx`: add `STREAM_INTERRUPTED`/`STREAM_CANCELLED` reducers (set `isStreaming:false`, reuse `stopped` flag); wire callbacks.
- [ ] 3.4 FE test for `runChatStream` dispatch of `interrupted`/`cancelled`.

## 4. Silent-drop observability

- [ ] 4.1 `internal/agent/event_iterator.go`: `slog.Debug` for `Exit`, non-summarization `CustomizedAction`, `CustomizedOutput`, `SessionEventVariant`; `slog.Warn` + continue for empty `MessageOutput`.

## 5. Verification

- [ ] 5.1 `make fmt`, `make vet`, `make lint`.
- [ ] 5.2 `make test` green; `internal/agent` and `internal/api/handler` coverage ≥ 70%.
- [ ] 5.3 Manual: error / cancel / interrupt each persist a turn row and emit the correct SSE event; normal completion unchanged (exactly one row, no double-persist).
