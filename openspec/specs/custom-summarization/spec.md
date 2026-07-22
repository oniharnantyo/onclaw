# custom-summarization Specification

## Purpose
TBD - created by archiving change custom-summarization-middleware. Update Purpose after archive.

## Requirements

### Requirement: Custom summarization middleware replaces Eino summarization
The system SHALL use a custom summarization middleware (`internal/agent/middlewares/summarization.go`) implementing `TypedChatModelAgentMiddleware[*schema.AgenticMessage]` instead of the Eino `summarization.NewTyped` middleware. The custom middleware SHALL perform the same functions: trigger detection, LLM summary generation, finalization, and persistence — but with full control over event timing and progress reporting.

#### Scenario: Summarization triggers on token threshold
- **WHEN** the input token count exceeds 80% of the context window
- **THEN** the middleware SHALL emit a compaction start event via the EventSink BEFORE initiating the LLM summary call

#### Scenario: Summarization triggers on message count backstop
- **WHEN** the message count exceeds 200 messages regardless of token count
- **THEN** the middleware SHALL emit a compaction start event via the EventSink BEFORE initiating the LLM summary call

### Requirement: Streaming LLM call for real-time progress
The custom summarization middleware SHALL use `model.Stream()` instead of `model.Generate()` for the summary LLM call. Each received chunk SHALL contribute to a progress percentage that is emitted via `EmitCompactionProgress(ctx, percent)` on the EventSink. Progress events SHALL be throttled to at most one per 200ms to avoid SSE flooding.

#### Scenario: Progress bar updates during LLM streaming
- **WHEN** summarization is triggered and the LLM begins streaming summary tokens
- **THEN** the system SHALL emit `compaction_progress` SSE events with an incrementing `progress` percentage (0-90%) as tokens arrive

#### Scenario: Progress reaches 100% on completion
- **WHEN** the LLM summary stream completes and the summary is persisted
- **THEN** the system SHALL emit a final `compaction_progress` event with `progress: 100` followed by a `compaction` event with `status: "completed"`

### Requirement: Streaming fallback to Generate
If `model.Stream()` returns an error, the middleware SHALL fall back to `model.Generate()` and emit binary start/end compaction events only (no progress updates). The middleware SHALL log a warning when falling back.

#### Scenario: Streaming unavailable
- **WHEN** the model does not support streaming for summarization
- **THEN** the middleware SHALL use `model.Generate()`, emit `compaction` start before the call and `compaction` completed after, with no `compaction_progress` events

### Requirement: Local finalizer replaces Eino finalizer
The custom middleware SHALL reimplement the Eino `buildInternalFinalizer` logic locally: split system messages from context messages, post-process the summary (inject recent user messages into `<all_user_messages>` block, add preamble, append transcript path), and return `[systemMsgs, processedSummary]`. The finalization behavior SHALL be identical to the Eino version.

#### Scenario: Summary message is properly finalized
- **WHEN** the LLM generates a summary response
- **THEN** the finalizer SHALL strip system messages, inject recent user messages, add the session continuation preamble, append the transcript path, and return the system messages followed by the processed summary

### Requirement: Retry on transient LLM failures
The custom middleware SHALL retry summary generation on transient failures with exponential backoff, matching Eino's existing behavior: up to 2 retries (3 total attempts) with exponential backoff and jitter.

#### Scenario: Retry on first failure
- **WHEN** the first summary generation attempt fails with a transient error
- **THEN** the middleware SHALL retry up to 2 additional times with exponential backoff before returning the error

### Requirement: Scrubbing and redaction preserved
The custom middleware SHALL use the existing `buildSummarizerInput` function from `summarization_scrub.go` to scrub tool results and calls before sending to the LLM. The summary output SHALL be redacted via `tools.RedactAgenticMessage` before persistence, matching current behavior.

#### Scenario: Tool results are scrubbed before summarization
- **WHEN** the conversation contains tool calls and results
- **THEN** the summarizer input SHALL have tool results replaced with stubs (e.g., `[write_file -> path]`, `[cleared read]`) before being sent to the LLM

### Requirement: Simplified Progress Bar UI without Stage Text
The frontend compaction status banner SHALL display only a static text label (e.g. "Compacting context...") alongside the progress bar, and SHALL NOT display verbose stage/progress text labels (such as "Saving summary...", "Finalizing...", or "X%"). The compaction progress state SHALL be used exclusively to drive the width of the progress bar fill.

#### Scenario: Compaction progress bar rendering
- **WHEN** context compaction is active
- **THEN** the UI SHALL render the progress bar driven by `state.compactionProgress` alongside the static label "Compacting context..."
