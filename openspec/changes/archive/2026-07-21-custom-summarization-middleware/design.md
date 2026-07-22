## Context

The Eino summarization middleware (`cloudwego/eino/adk/middlewares/summarization`) handles compaction but exposes no hook before the LLM call. Its `Callback` fires after `Summarize()` completes, which includes the full `model.Generate()` call (10-30s). This means any progress event we emit from the callback arrives too late — the user has been staring at a blank screen during the slowest phase.

The middleware chain runs in order: `[inputSafety, summarization, history, fs, fsToggle, fsError, memory, skills, hooks]`. The summarization middleware's `BeforeModelRewriteState` checks `shouldSummarize()`, and if triggered, calls `Summarize()` synchronously which blocks on the LLM.

## Goals / Non-Goals

**Goals:**
- Emit compaction progress events BEFORE and DURING the LLM summary call
- Use streaming (`model.Stream()`) for real-time token-level progress
- Full ownership of the summarization pipeline (no dependency on Eino's unexported internals)
- Clean progress bar UI replacing the current spinner
- Graceful fallback if streaming is unavailable

**Non-Goals:**
- Changing summarization behavior (same prompt, same scrubbing, same finalization)
- Adding multi-stage text labels ("Generating...", "Saving...") — just a progress bar
- Modifying the history, memory, or filesystem middleware chains
- Supporting non-streaming models for summarization (fallback to Generate covers this)

## Decisions

### D1: Custom middleware replacing Eino summarization

**Decision:** Create `internal/agent/middlewares/summarization.go` implementing `TypedChatModelAgentMiddleware[*schema.AgenticMessage]` directly.

**Why not wrap Eino:** Wrapping would still hit the same callback-after-LLM problem. A pre-compaction detection middleware could emit start early, but wouldn't get real progress during the LLM call.

**Why not modify Eino:** The middleware is an external dependency; modifying it creates maintenance burden and fork drift.

**Alternatives considered:**
- Pre-compaction detection middleware (Option A from exploration): Simpler but only gives binary start/end, no progress bar
- Streaming wrapper around Eino: Eino's `generateWithRetry` uses `Generate()`, not `Stream()`, so we can't intercept chunks

### D2: Streaming LLM call for progress

**Decision:** Use `model.Stream()` to get token chunks during summary generation.

**How progress works:**
1. Estimate target token count (conservatively: 15% of input token count, minimum 500)
2. As each chunk arrives via `stream.Recv()`, accumulate text length
3. Compute `percent = min(90, (received / estimated) * 90)` — cap at 90% during streaming
4. Emit `EmitCompactionProgress(ctx, percent)` on each chunk (throttled to avoid SSE flooding: at most once per 200ms)
5. On stream completion, emit 95% (persist phase), then 100% on finalize

**Why cap at 90%:** The remaining 10% covers finalization and persistence — the user should see the bar reach 100% only when everything is truly done.

### D3: Local finalizer reimplemented from Eino

**Decision:** Reimplement the Eino `buildInternalFinalizer` logic locally (~50 lines).

**What it does:**
1. Split system messages from context messages
2. Post-process summary: inject recent user messages into `<all_user_messages>` block, add preamble ("This session is being continued from..."), append transcript path
3. Return `[systemMsgs, processedSummary]`

**Why not import from Eino:** The finalizer functions are unexported. We already have `buildSummarizerInput` (scrubbing) in `summarization_scrub.go` — the finalizer is the missing piece.

### D4: Progress event on EventSink

**Decision:** Add `EmitCompactionProgress(ctx context.Context, percent int)` to the `EventSink` interface.

**SSE format:**
```
event: compaction_progress
data: {"progress": 42}
```

**Frontend:** `onCompactionProgress` callback updates `compactionProgress` state, renders a thin CSS progress bar.

### D5: Streaming fallback

**Decision:** If `model.Stream()` returns an error, fall back to `model.Generate()` and emit binary start/end only (no progress updates).

**Rationale:** Some provider adapters or model configurations may not support streaming. The progress bar degrades gracefully to a spinner (100% on completion).

## Risks / Trade-offs

- **[Progress estimation inaccuracy]** → The estimated target may be wrong (actual summary longer/shorter than 15% of input). Mitigation: cap at 90% during streaming, always reach 100% on finalize. The bar may slow down or jump, but never lies about completion.
- **[SSE flooding from chunk-level events]** → Emitting progress on every stream chunk could overwhelm the SSE writer. Mitigation: throttle to at most one progress event per 200ms.
- **[Eino finalizer divergence]** → Reimplementing the finalizer means we must keep it in sync if Eino updates. Mitigation: the finalizer is ~50 lines of straightforward string processing; we already own the summarizer input (scrubbing). The risk is low.
- **[Streaming cost]** → Streaming may cost slightly more than batch generation on some providers. Mitigation: summarization is infrequent (once per ~80% context fill), cost difference is negligible.