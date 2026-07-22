## 1. EventSink Progress Interface

- [x] 1.1 Add `EmitCompactionProgress(ctx context.Context, percent int)` to `EventSink` interface in `internal/agent/middlewares/event.go`
- [x] 1.2 Implement `EmitCompactionProgress` as no-op in `NoOpEventSink` (`internal/agent/middlewares/event.go`)
- [x] 1.3 Implement `EmitCompactionProgress` in `eventSinkWrapper` in `internal/agent/agent.go`
- [x] 1.4 Add `EmitCompactionProgress` to `NoOpEventSink` in `internal/agent/event.go`
- [x] 1.5 Add `EmitCompactionProgress` to `sseEventSink` in `internal/api/handler/chat.go` — emit SSE event `compaction_progress` with `{"progress": percent}`

## 2. Custom Summarization Middleware

- [x] 2.1 Create `internal/agent/middlewares/summarization.go` with `SummarizationMiddleware` struct implementing `TypedChatModelAgentMiddleware[*schema.AgenticMessage]`
- [x] 2.2 Implement `BeforeModelRewriteState`: check token threshold and message backstop using existing `inputTokenCounter` logic, emit `EmitCompactionStart` when triggered
- [x] 2.3 Implement `summarizeWithStream`: call `model.Stream()` with scrubbed input from `buildSummarizerInput`, accumulate chunks, emit throttled `EmitCompactionProgress` events
- [x] 2.4 Implement streaming fallback: if `Stream()` fails, fall back to `model.Generate()` with binary progress only
- [x] 2.5 Implement local finalizer: split system/context messages, inject `<all_user_messages>`, add preamble and transcript path — matching Eino's `buildInternalFinalizer` behavior
- [x] 2.6 Implement retry logic: up to 2 retries with exponential backoff on transient LLM failures
- [x] 2.7 Wire the new middleware into `buildMiddleware()` in `agent.go`, replacing the Eino `summarization.NewTyped` call

## 3. Agent Integration

- [x] 3.1 Update `buildSummarizationMiddleware` in `agent.go` to construct the custom middleware with EventSink, chat model, trigger tokens, transcript path, and summarizer input builder
- [x] 3.2 Remove the `EmitCompactionStart` from the existing callback — the custom middleware handles start events; keep `EmitCompactionEnd` in the callback for completion
- [x] 3.3 Ensure the existing `handleSummarization` function is called from the custom middleware's completion path (memory extraction, summary persistence, transcript boundary)

## 4. Frontend Progress Bar

- [x] 4.1 Add `compactionProgress: number` to `ChatState` in `ChatProvider.tsx`
- [x] 4.2 Add `SET_COMPACTING_PROGRESS` action to the reducer, update `compactionProgress` and reset on `SET_COMPACTING`
- [x] 4.3 Add `onCompactionProgress` callback in `runChatStream.ts` to handle `compaction_progress` SSE events
- [x] 4.4 Update the compaction banner in `Chat.tsx`: replace spinner with a thin CSS progress bar driven by `state.compactionProgress`
- [x] 4.5 Add progress bar CSS in `index.css`: thin horizontal bar with animated fill

## 5. Cleanup and Diagnostics

- [x] 5.1 Remove diagnostic `console.log` from `Chat.tsx`, `runChatStream.ts`, and `ChatProvider.tsx`
- [x] 5.2 Remove diagnostic `slog.Info`/`slog.Warn` from `handler/chat.go` and `agent.go` event sink wrapper

## 6. Testing

- [x] 6.1 Write unit tests for `SummarizationMiddleware`: trigger detection, streaming progress, fallback, finalization
- [x] 6.2 Verify existing agent tests pass with the new middleware
- [x] 6.3 Manual test: send 15+ messages, trigger summarization, verify progress bar appears and fills during LLM call