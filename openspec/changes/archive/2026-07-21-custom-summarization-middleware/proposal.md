## Why

The compaction banner appears **after** summarization completes, not during it. The Eino summarization middleware's callback fires after the LLM call finishes (10-30s), so `EmitCompactionStart` only triggers during the ~1s DB save. Users see no feedback during the longest phase of compaction.

## What Changes

- Replace the Eino summarization middleware with a custom one that gives full control over event timing
- Use `model.Stream()` instead of `model.Generate()` for the LLM summary call, enabling real-time progress tracking via token chunks
- Add a new `EmitCompactionProgress(ctx, percent)` event so the UI can show a progress bar instead of a spinner
- Reimplement the Eino finalizer (postProcessSummary, system/context message splitting) locally for full ownership
- Update the frontend banner from a spinner to a thin progress bar driven by `compaction_progress` SSE events
- Fallback: if streaming fails, degrade to `Generate()` with binary start/end events

## Capabilities

### New Capabilities
- `custom-summarization`: Custom summarization middleware with streaming LLM call, progress reporting, and local finalizer — replaces the external Eino summarization middleware

### Modified Capabilities
- `chat-ui`: Compaction banner changes from spinner to progress bar, new `compaction_progress` SSE event type
- `conversation-history`: Summarization trigger detection now happens in a separate pre-compaction middleware before the main summarization step

## Impact

- **Backend (Go):** New file `internal/agent/middlewares/summarization.go`, modified `agent.go` (middleware wiring + callback), modified `handler/chat.go` (new SSE event), modified `event.go` and `middlewares/event.go` (new `EmitCompactionProgress` method)
- **Frontend (TS):** Modified `ChatProvider.tsx` (progress state), modified `Chat.tsx` (progress bar UI), modified `runChatStream.ts` (new event handler), modified `index.css` (progress bar styles)
- **Dependencies:** No new external dependencies; reuses existing `model.Stream()` from Eino