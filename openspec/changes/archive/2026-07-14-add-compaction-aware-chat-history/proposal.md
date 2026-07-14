## Why

When a conversation is compacted mid-chat, the summary is stored as an ordinary turn
row in `conversation_messages`. The UI renders it through the same path as any assistant
message, so it appears as an unexplained assistant bubble mid-transcript (a "mystery
recap") while all the original compacted messages remain visible above it. At the same
time the context meter reads the post-compaction turn's `prompt_tokens`, so it drops from
~80% to ~25% with no indication that compaction occurred. The agent and the user see
compaction very differently, and neither is told it happened.

Separately the summarization path has two latent weaknesses: summary generation is
configured with zero retries (`Retry` defaults to none, so a transient failure fails the
turn), and the default trigger anchors on `TotalTokens` (prompt + completion), which
over-counts input fill by the last answer's length — significant for a coding agent that
emits long completions, so compaction fires earlier than intended.

A third weakness is structural: summarization is applied *blindly*. The trigger fires on the
total token count, then compresses only conversation history — the system prompt and tool
schemas are never summarized. When that fixed floor already exceeds the budget (e.g. a large
persona on a small `max_context_tokens`), summarization is helpless but still churns every
turn, masking the real overflow. Observed: `max_context_tokens: 5000` with a ~7400-token
persona+tools floor sent a ~8159-token request with summarization firing pointlessly.

## What Changes

- **Flag summary turn rows.** `SaveSummary` SHALL mark summary rows with an `is_summary`
  flag; `TurnRow` and `ListTurns` SHALL expose it, so every compaction (including
  superseded ones after re-compaction) is identifiable for rendering and metadata.
- **Render a compaction boundary marker.** Summary turns SHALL render as a marker — a
  divider ("Earlier conversation summarized") with the summary text available in a
  collapsible region — never as a normal assistant bubble. Non-summary messages SHALL
  remain fully visible (append-only retention is unchanged).
- **Surface compaction metadata.** The messages API SHALL return conversation-level
  `compaction_count` and `last_compaction_at`; the context meter SHALL annotate the
  one-time drop when `compaction_count` increases, so the meter reads as an event rather
  than a glitch.
- **Count input tokens for the trigger.** The summarization middleware SHALL use a
  `TokenCounter` that anchors on `PromptTokens` (not `TotalTokens`) plus a
  `ContextMessages` backstop, so compaction fires at the intended input fill.
- **Make summarization resilient.** The summarization middleware SHALL enable `Retry` so a
  transient summary-generation failure does not fail the turn.
- **Let the agent re-read compacted detail.** The summarization middleware SHALL set
  `TranscriptFilePath` to a readable transcript of the compacted range, so the summary can
  point the model back at exact prior detail.
- **Gate the input floor before summarizing.** Agent assembly SHALL estimate the fixed floor
  (system prompt + tool schemas) and fail fast when it reaches a safety limit set *below* the
  summarization trigger (`floorSafetyFraction × context_window`, default 0.5 vs the 0.8
  trigger), so an oversized floor is rejected with an actionable error instead of summarized
  blindly. A per-turn preflight middleware re-checks it as a runtime net.

## Capabilities

### Modified Capabilities

- `chat-ui`: adds a compaction boundary marker requirement and a context-meter
  compaction-annotation requirement.
- `conversation-history`: adds requirements for summary-row flagging, compaction-metadata
  surfacing, trigger input-token counting with a message-count backstop, summarization
  retry, a re-readable compacted transcript, and an input-floor safety guard that fails fast
  below the summarization trigger.

## Impact

**Affected code:**
- `internal/store/sqlite/db.go` (migration: `is_summary` column)
- `internal/store/sqlite/conversation.go` (`SaveSummary` sets the flag; `ListTurns`
  returns it; metadata query)
- `internal/store/types.go` (`TurnRow.IsSummary`)
- `internal/api/{service,handler}/conversation.go` (expose `is_summary`,
  `compaction_count`, `last_compaction_at`)
- `internal/agent/agent.go` (summarization `TypedConfig`: `TokenCounter`,
  `Trigger.ContextMessages`, `Retry`, `TranscriptFilePath`)
- `web/src/types/chat.ts`, `web/src/components/ChatProvider.tsx`,
  `web/src/components/chat/Renderers.tsx`, `web/src/components/chat/groupBlocks.ts`
  (marker rendering + meter annotation)
- `internal/agent/agent.go` (build-time floor guard; preflight registered at handlers index 0)
- `internal/agent/input_safety.go` (floor estimator)
- `internal/agent/middlewares/input_safety.go`, `input_safety_middleware.go` (threshold,
  sentinel, per-turn preflight)
- `internal/api/handler/chat.go` (sentinel mapped to HTTP 400)

**Affected systems:**
- Conversation history persistence and replay (read path unchanged; one new column)
- Chat transcript rendering and the context meter
- Summarization trigger timing and resilience

**Dependencies:**
- No new external dependencies. Uses Eino's existing `TypedConfig` fields (`TokenCounter`,
  `TriggerCondition.ContextMessages`, `Retry`, `TranscriptFilePath`).

**Non-goals:**
- No change to agent-side bounded replay (`summary + last 3`) — already correct.
- No client-side tokenizer; the trigger keeps the anchor + chars/4-increment shape, only
  swapping the anchor field.
- No change to append-only retention; compacted originals remain for audit.
- Not introducing Anthropic server-side compaction (`compact_20260112`) — onclaw stays
  provider-agnostic.
- Not a hard cap on a single turn's request size; summarization still owns the 80% history-trim
  line. The floor guard only rejects agents whose *fixed* input cannot fit.