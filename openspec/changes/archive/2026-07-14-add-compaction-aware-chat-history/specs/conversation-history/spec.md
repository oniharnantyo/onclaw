## ADDED Requirements

### Requirement: Summary turn rows are flagged
The system SHALL mark every summary turn row with an `is_summary` flag at insert time, so each compaction — including superseded summaries after a re-compaction — is identifiable independently of the active-summary pointer. `SaveSummary` SHALL set the flag; `TurnRow` SHALL carry an `IsSummary` field; `ListTurns` SHALL return it. Non-summary turn rows SHALL carry `is_summary = false`. The flag is for rendering and metadata only; bounded replay continues to use `summary_message_id` and `summary_until_seq`.

#### Scenario: A summary row is flagged
- **WHEN** summarization compaction persists a summary turn row
- **THEN** that row's `is_summary` is true
- **AND** a normal turn row's `is_summary` is false

#### Scenario: Re-compaction flags every summary
- **WHEN** a conversation is compacted a second time
- **THEN** both the superseded and the new summary rows have `is_summary = true`
- **AND** bounded replay still injects only the active summary plus its tail

### Requirement: Compaction metadata is surfaced to clients
The messages endpoint SHALL return conversation-level `compaction_count` (the number of `is_summary` rows) and `last_compaction_at` (the `created_at` of the most recent summary row), alongside the per-turn rows, so clients can detect and annotate compaction without scanning message content.

#### Scenario: Metadata reflects a compaction
- **WHEN** a conversation has been compacted once and its messages are listed
- **THEN** the response carries `compaction_count = 1` and a non-empty `last_compaction_at`

### Requirement: Summarization trigger counts input tokens
The summarization middleware SHALL be configured with a `TokenCounter` whose baseline is the most recent assistant message's `PromptTokens` (input tokens), not `TotalTokens`, so the trigger measures context input fill rather than input plus the prior completion. The increment for messages newer than that baseline and for tool definitions SHALL continue to use the framework's character-based estimate.

#### Scenario: A long completion does not inflate the trigger
- **WHEN** the most recent assistant turn produced a large completion
- **THEN** the summarization token measurement reflects that turn's input tokens, not input plus completion

### Requirement: Summarization is resilient and message-bounded
The summarization middleware SHALL enable `Retry` so a transient summary-generation failure does not fail the agent turn, and SHALL set a `ContextMessages` backstop so summarization also triggers when the message count exceeds a bounded ceiling regardless of token count.

#### Scenario: A transient summary failure is retried
- **WHEN** summary generation fails transiently mid-turn
- **THEN** the middleware retries it up to the configured bound rather than failing the turn

#### Scenario: A message-count backstop triggers summarization
- **WHEN** a conversation exceeds the configured message-count ceiling before the token threshold
- **THEN** summarization triggers

### Requirement: The compacted transcript is re-readable by the agent
The summarization middleware SHALL set `TranscriptFilePath` to a readable transcript covering the compacted range (`sequence_num <= summary_until_seq`), so the generated summary can direct the model to re-read exact prior detail that the summary abbreviates.

#### Scenario: The summary cites a readable transcript
- **WHEN** compaction produces a summary
- **THEN** the summary references a transcript path the agent can read to recover compacted detail

### Requirement: Agent build refuses an oversized input floor
Agent assembly SHALL estimate the fixed input floor — the system prompt plus the marshaled tool schemas — and SHALL fail fast when that floor reaches a safety limit computed as `floorSafetyFraction * contextWindow` (default 0.5), which is below the 0.8 summarization trigger. The failure SHALL return an actionable error naming the estimated floor, the limit, and the context window, so the operator can trim the system prompt/persona, disable tools, or raise `max_context_tokens`.

#### Scenario: An oversized floor is refused at build
- **WHEN** an agent's fixed floor (system prompt + tools) reaches the floor safety limit for its context window
- **THEN** agent assembly fails fast with an actionable error
- **AND** no agent turn is run

#### Scenario: A normal floor passes
- **WHEN** an agent's fixed floor is below the floor safety limit
- **THEN** agent assembly succeeds and the agent runs normally

### Requirement: Per-turn input-safety preflight
A middleware ordered before summarization SHALL re-estimate the tool floor from the live tool list each turn and fail the turn fast when the floor reaches the safety limit, so an oversized floor never reaches a blind summarization cycle. This is the authoritative runtime complement to the build-time guard.

#### Scenario: A runtime-discovered oversized floor fails the preflight
- **WHEN** the live tool list pushes the floor to the safety limit on a turn
- **THEN** the turn fails fast with the input-floor error and summarization is skipped