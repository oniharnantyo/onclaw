# conversation-history

## Purpose

Persist full conversation messages (the entire `*schema.Message`) to the SQLite
database onclaw already opens, replay them into the agent before each run for
multi-turn memory, and keep replay bounded via durable summarization compaction.
History replaces the former per-session `.jsonl` transcript and is the substrate
for future durable summarization and vector search.
## Requirements
### Requirement: History is replayed into the agent before each run
The system SHALL, before each agent run, reconstruct the message list from stored turn rows by concatenating each turn's `message` array in `sequence_num` order and inject it before the new user message so the agent has multi-turn memory. Replay SHALL be bounded: with no summary present it SHALL send all turns; once a summary exists it SHALL send the summary message plus the last 3 turns past the summary's coverage cursor (`tailTurnWindow = 3`), and SHALL NOT load the full raw session into memory. `onclaw chat` SHALL reuse one conversation across all turns of a REPL session; `onclaw run` SHALL create one conversation per invocation.

#### Scenario: A REPL session remembers prior turns
- **WHEN** a user runs `onclaw chat`, asks something in turn 1, and refers to it in turn 2
- **THEN** the agent answers using the history reconstructed from the shared conversation

#### Scenario: Replay sends all turns until the context threshold
- **WHEN** a conversation has several turns and no summary has been produced
- **THEN** every turn's message array is concatenated and sent to the model

#### Scenario: Replay is bounded to summary + last 3 turns after compaction
- **WHEN** a conversation has been compacted and a new turn begins
- **THEN** only the latest summary and the last 3 turns past its coverage cursor are injected, not the full raw history

### Requirement: Summarization compaction is durable across runs
The system SHALL persist the summary and a coverage cursor whenever summarization compacts history, so compacted turns are represented by the summary on subsequent replays. The coverage cursor SHALL record the highest turn `sequence_num` the summary covers. Replay SHALL inject the summary followed by the last 3 turns with `sequence_num` beyond the cursor; compacted originals SHALL remain in the database for audit but SHALL NOT be re-injected into the model.

#### Scenario: Compaction persists and is reused on replay
- **WHEN** a long conversation exceeds the summarization threshold and compaction fires
- **THEN** a summary turn row is persisted, the coverage cursor advances, and the next turn replays the summary plus the last 3 turns instead of the full history

### Requirement: Persisted history excludes resolved secret values

The system SHALL redact known secret values from message content, tool-call arguments, and tool
results before persisting them, using the same redaction applied to transcripts today
(cross-ref `providers`). The conversation store SHALL NOT receive or hold resolved secret
values.

#### Scenario: A message containing a secret is redacted

- **WHEN** a tool result contains a resolved secret value
- **THEN** the persisted row contains the redacted form, not the secret

### Requirement: Conversations can be enumerated
The system SHALL provide `ConversationStore.ListConversations(ctx) ([]*ConversationRow, error)` returning each conversation's id, agent name, created and updated timestamps, turn count, and the first turn's `question` as a preview. The store interface, the `ConversationRow` DTO, and the SQLite implementation SHALL follow the existing contract/types/implementation separation. This enumeration supports the web UI's conversation list and any future listing surface.

#### Scenario: conversations are listed with counts and preview
- **WHEN** a conversation with several turns exists and `ListConversations` is called
- **THEN** the result includes that conversation's id, agent name, timestamps, turn count, and the first turn's question

#### Scenario: an empty store lists nothing
- **WHEN** `ListConversations` is called and no conversations exist
- **THEN** it returns an empty (or nil) slice and no error

### Requirement: Conversation history is full-text searchable
The conversation store SHALL support FTS5 search over each turn's `question` and `answer` text so the agent can recall specific past conversations via `session_search`, independent of the live history window. FTS SHALL NOT index the raw `message` JSON array.

#### Scenario: A past turn is found by keyword
- **WHEN** `session_search` runs a query that matches a past turn's question or answer
- **THEN** that turn is returned (with its question and answer) ranked by FTS5 relevance

### Requirement: Memory flush precedes summary persistence

The summarization path SHALL offer a flush hook that runs memory extraction over the messages
being compacted before the compaction summary is persisted to the conversation store.

#### Scenario: The flush hook runs before SaveSummary

- **WHEN** the summarization middleware persists a compaction summary
- **THEN** the memory flush hook has already run over the compacted message range

### Requirement: Response ID must never be empty
The system SHALL guarantee that every persisted conversation turn has a non-empty `response_id` field when possible.

#### Scenario: New conversation turn with provider response ID
- **WHEN** a conversation turn is persisted with provider-supplied response ID
- **THEN** `response_id` field MUST contain a non-empty string
- **AND** `previous_response_id` references the prior turn's `response_id`

#### Scenario: Fallback to Eino message ID
- **WHEN** provider does not supply a response ID
- **AND** Eino framework has populated `_eino_msg_id` in message metadata
- **THEN** system MUST use `_eino_msg_id` as `response_id`
- **AND** `response_id` field MUST NOT be empty

#### Scenario: Response chaining across providers
- **WHEN** conversation spans multiple providers (e.g., OpenAI → Bedrock)
- **THEN** each turn MUST have a valid `response_id` regardless of provider
- **AND** `previous_response_id` MUST correctly reference the immediate predecessor

#### Scenario: Graceful degradation when no ID available
- **WHEN** neither provider nor Eino metadata provides a response ID
- **THEN** system MAY persist with empty `response_id`
- **AND** system MUST log a warning about missing response ID

### Requirement: Conversation history is persisted as turn rows in SQLite
The system SHALL persist conversation history as **one row per turn** (a turn being a complete exchange ending in the final assistant response). Each turn row SHALL carry the turn's messages as a JSON array of the full `*schema.AgenticMessage` deltas (role, content blocks — including assistant text, reasoning, function tool calls, and function tool results — response metadata, and the message `Extra` map) produced during that turn, a monotonically increasing per-conversation `sequence_num`, the `model` used, per-turn `prompt_tokens`/`completion_tokens`/`total_tokens`, denormalized `question` (first user block text) and `answer` (last assistant block text), and `response_id`/`previous_response_id` for follow-up threading. **System-role messages SHALL NOT be included in the persisted array**: the agent instruction (re-injected by the framework each turn) and any middleware-injected system context (e.g. curated memory) are re-applied on every run, so the history middleware SHALL exclude messages with `role == system` when accumulating a turn's messages. Turns SHALL be grouped into conversations; each conversation SHALL belong to an agent. Persistence SHALL be append-only; the run loop SHALL NOT mutate or delete existing rows. The store package SHALL remain free of eino imports; the agent layer SHALL perform `*schema.AgenticMessage` <-> JSON conversion and secret redaction before persistence. **BREAKING:** rows previously persisted one-message-per-row are not read back; this is a clean format break (pre-release).

#### Scenario: A turn is persisted as one row with its message array
- **WHEN** a turn runs that calls a tool and returns an answer
- **THEN** the database holds exactly one row for that turn whose `message` array contains the user, assistant (with tool-call content blocks), tool-result, and final-assistant messages, and whose `sequence_num` is one greater than the prior turn's

#### Scenario: Tool calls are stored within the assistant message in the array
- **WHEN** the assistant emits a message that requests a tool call
- **THEN** the tool call is stored inside that assistant message's content blocks within the turn's `message` array rather than as a separate row

#### Scenario: System messages are not persisted
- **WHEN** the agent state for a turn contains a system-role message (the framework's instruction or middleware-injected system context) alongside the user, assistant, tool-call, and tool-result messages
- **THEN** the persisted turn row's `message` array SHALL contain no system-role messages
- **AND** the array SHALL contain only the user, assistant, tool-call, and tool-result messages produced during the turn

### Requirement: A turn is committed as one row at turn end
The system SHALL commit a turn as a single row when the turn completes (at the agent turn-end hook), accumulating the turn's new messages in memory during the run and writing them once via a history middleware — not the run loop, and not eagerly per message. The committed row SHALL record the model, per-turn token usage read from the final assistant message's response metadata, the `response_id` of the turn, the `previous_response_id` of the prior turn (empty for the first turn), and extracted `question`/`answer` text.

#### Scenario: A complete turn is committed once
- **WHEN** a turn completes with a final assistant response
- **THEN** exactly one turn row is written containing all of the turn's messages, model, token usage, response ids, and question/answer

#### Scenario: A turn interrupted before completion leaves no row
- **WHEN** a turn is interrupted before the turn-end hook fires
- **THEN** no turn row is written for that turn (a turn is a complete exchange ending in a response)

### Requirement: Per-turn model and token usage are recorded
Each persisted turn row SHALL record the model used for the turn and the turn's `prompt_tokens`, `completion_tokens`, and `total_tokens`, read from the final assistant message's response metadata. Where the active adapter does not populate usage metadata, the values SHALL be stored as zero rather than absent.

#### Scenario: Token usage is captured when the adapter provides it
- **WHEN** a turn completes and the adapter populated response metadata with usage
- **THEN** the turn row's token columns reflect prompt, completion, and total tokens

#### Scenario: Token usage is zero when the adapter is a stub
- **WHEN** a turn completes under a stub adapter that does not populate response metadata
- **THEN** the turn row's token columns are zero

### Requirement: Response ids thread follow-up turns
Each turn row SHALL carry a `response_id` identifying the turn and a `previous_response_id` equal to the prior turn's `response_id` (empty for the first turn of a conversation), so follow-up turns can be chained. These ids SHALL be surfaced via the chat API but SHALL NOT alter the live history reconstruction, which always sends reconstructed (bounded) history.

#### Scenario: Follow-up turns chain response ids
- **WHEN** a second turn is committed in a conversation
- **THEN** its `previous_response_id` equals the first turn's `response_id`, and the first turn's `previous_response_id` is empty

### Requirement: The chat API surfaces turn metadata and accepts a previous response id
`POST /api/chat` SHALL accept an optional `previous_response_id` request field and SHALL emit a terminal `turn` SSE event carrying the new turn's `conversation_id`, `sequence_num`, `response_id`, `previous_response_id`, `model`, and token usage, so the client can chain follow-ups and display per-turn metadata.

#### Scenario: The client receives the turn's response id
- **WHEN** a chat turn completes
- **THEN** the SSE stream emits a `turn` event whose `response_id` identifies the turn for the next follow-up

#### Scenario: The client may pass a previous response id
- **WHEN** the client sends `previous_response_id` on a follow-up
- **THEN** it is accepted and persisted as the new turn's `previous_response_id`

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

