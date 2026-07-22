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

### Requirement: Response ID fallback is relaxed
The system SHALL attempt to read the response ID from the final assistant message's `_eino_msg_id` in `Extra`. Provider-supplied extensions (state-only response ID) are dropped.

#### Scenario: Fallback to Eino message ID
- **WHEN** Eino framework has populated `_eino_msg_id` in the assistant message metadata
- **THEN** the system MUST use `_eino_msg_id` as `response_id`
- **AND** the `response_id` field MUST NOT be empty

#### Scenario: Graceful degradation when no ID is available
- **WHEN** the final assistant message lacks `_eino_msg_id`
- **THEN** the system MAY persist with an empty `response_id`
- **AND** the system MUST log a warning about the missing response ID

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

### Requirement: A turn is committed as one row driven by the entrypoint
The system SHALL commit a turn as a single row when the Eino agent run completes, driven by the entrypoint calling `SessionManager.CommitTurn` (no framework persistence middleware is used in the agent runner). The committed row SHALL record the model, estimated token usage, the `response_id` of the turn, the `previous_response_id` of the prior turn (empty for the first turn), and extracted `question`/`answer` text.

#### Scenario: A complete turn is committed once post-run
- **WHEN** a turn run completes and the entrypoint drains the event iterator
- **THEN** exactly one turn row is written containing all of the turn's messages, model, estimated token usage, response ids, and question/answer

### Requirement: Per-turn model and estimated token usage are recorded
Each persisted turn row SHALL record the model used for the turn and the turn's `prompt_tokens`, `completion_tokens`, and `total_tokens`, estimated via character-based count (characters / 4) over the turn's messages.

#### Scenario: Token usage is estimated using character counts
- **WHEN** a turn completes and is committed
- **THEN** the turn row's token columns reflect the estimated prompt, completion, and total tokens based on character length

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

### Requirement: Replayed history excludes reasoning content
The system SHALL strip `Reasoning`/thinking content blocks from messages loaded for replay before
each agent run, so the live context the agent reasons over never contains prior reasoning. The
strip SHALL be applied deterministically at history load and SHALL NOT mutate already-loaded
messages on subsequent turns, so the replayed prefix remains stable for prefix caching. New turn
rows SHALL NOT persist reasoning blocks into the conversation history; reasoning captured for
observability SHALL travel the event/callback path, not the replay substrate.

#### Scenario: A replayed turn drops its reasoning blocks
- **WHEN** a persisted turn carries an assistant message with both generated text and a reasoning block, and the turn is replayed into the agent
- **THEN** the injected message array contains the generated text but no reasoning block

#### Scenario: The replayed prefix is stable across turns
- **WHEN** a turn is replayed on turn N and again on turn N+1
- **THEN** the byte representation of the replayed (non-new) messages is identical across the two turns

### Requirement: Summarizer input is scrubbed of bulky mechanics
When summarization compacts a range, the system SHALL construct the summarization model's input so
that old tool-result content, reasoning, and raw tool-call mechanics within the compacted range are
removed or condensed before the summary model sees them. The scrub SHALL apply only to the range
being compacted (anchored to the coverage cursor), never to the live replay prefix, so that prefix
caching remains effective.

#### Scenario: Old tool results are condensed for the summarizer
- **WHEN** summarization runs over a range containing large tool-result bodies
- **THEN** the summarization model input contains stubs or gists in place of those bodies, not the verbatim content

#### Scenario: The live replay prefix is not mutated by summarizer scrubbing
- **WHEN** summarizer-input scrubbing is applied on a compaction
- **THEN** the messages replayed to the agent on the next turn are unchanged by the scrub (only the summary message replaces the compacted range)

### Requirement: Tool-result stubs preserve durable pointers
When a tool result is condensed for the summarizer, the system SHALL preserve a durable pointer to
the underlying artifact: for file-producing tools (`write_file`, `edit_file`, or a result
saved to a file) the stub SHALL carry the destination path verbatim; for skill invocations the stub
SHALL carry the skill name and a one-line outcome; for other tools the stub SHALL carry the tool
identity. Tool-call arguments for file-producing tools SHALL be trimmed to the path, dropping bulky
`content`/`new_string` payloads.

#### Scenario: A file write is summarized by its path
- **WHEN** the compacted range contains a `write_file` call that wrote `src/auth/login.go`
- **THEN** the summarizer input references `src/auth/login.go` and does not include the written body

#### Scenario: A prior skill use survives compaction
- **WHEN** the compacted range contains a skill invocation
- **THEN** the summarizer input records the skill name and a short outcome, so the fact and result of the skill use are preserved

### Requirement: Summarization uses a recall-first onclaw-tailored prompt
The summarization middleware SHALL be configured with a custom `UserInstruction` that instructs the
model to preserve verbatim architectural decisions and their rationale, file paths and directories,
skill uses, unresolved bugs and error messages/codes, and identifiers (UUIDs, hashes, tokens, URLs,
function/type names); to treat tool-result stubs as pointers rather than full content; to preserve
`<redacted>` secret placeholders as-is; and to discard redundant tool bodies and reasoning. The
prompt SHALL forbid tool calls during summarization.

#### Scenario: The summary preserves paths and an unresolved bug
- **WHEN** compaction summarizes a range that referenced specific file paths and an unresolved error
- **THEN** the resulting summary contains those paths and the error verbatim

#### Scenario: Redacted secrets are not reconstructed
- **WHEN** the compacted range contains `<redacted>` placeholders for secret values
- **THEN** the summary keeps them as `<redacted>` and does not invent values

### Requirement: Prefix caching is enabled per provider where supported
The system SHALL enable conversation prefix caching for providers that support it, so the stable
replayed prefix is amortized across turns rather than re-billed in full. Caching SHALL be wired per
adapter (Anthropic cache control, OpenAI-compatible automatic prefix caching, Gemini context
caching) and SHALL degrade gracefully to re-billing where a provider offers no caching. The
cache-stability invariant (no per-turn mutation of replayed messages) SHALL be preserved so caching
remains effective.

#### Scenario: A cacheable prefix is reused across turns
- **WHEN** a provider supports prefix caching and a conversation replays a stable prefix across consecutive turns
- **THEN** the provider reports cache hits on the stable portion of the prefix

#### Scenario: A non-caching provider still functions
- **WHEN** a provider offers no prefix caching
- **THEN** the agent still replays and runs correctly, re-billing the prefix

#### Scenario: Gemini context caching is wired at runtime
- **WHEN** a Gemini profile has prefix caching enabled
- **THEN** a cached-content resource is created from the stable prefix and referenced on subsequent turns, so the prefix is cached rather than re-billed (until this is wired, Gemini degrades to re-billing — graceful)

#### Scenario: Anthropic cache breakpoint covers the stable prefix
- **WHEN** an Anthropic request is sent across consecutive turns
- **THEN** the cache breakpoint is placed at the stable system + tools + history prefix boundary (not only the final message block), so the prefix is explicitly cached

### Requirement: The agent is re-grounded with recently accessed files after compaction
After compaction, the system SHALL surface the most recently accessed workspace file paths
(default 5) alongside the summary, so the agent can re-read current source on demand rather than
relying on stale compacted copies. File-access recency SHALL be tracked from filesystem-tool usage
into a per-conversation recency list. The transcript file SHALL be demoted to a fallback recall path.

#### Scenario: Recent files follow the summary
- **WHEN** a conversation is compacted and a new turn begins
- **THEN** the agent receives the summary plus a note listing the most recently accessed file paths

### Requirement: The transcript is written lazily
The system SHALL NOT write the per-conversation transcript file on every compaction. It SHALL build
and write the transcript only when the agent (or another recall path) actually requests the
compacted range's verbatim detail. The append-only DB remains the source of truth for the
compacted range.

#### Scenario: A compaction does not eagerly write the transcript
- **WHEN** summarization compacts a range
- **THEN** no transcript file is written unless the transcript path is subsequently read

#### Scenario: A deep-recall request builds the transcript on demand
- **WHEN** the agent reads the transcript path after a compaction
- **THEN** the transcript file is built from the append-only DB for the covered range and returned

### Requirement: Summarization quality is measured by probes
The system SHALL include a probe harness that runs summarization over captured real agent traces and
scores recall (file paths, decisions, unresolved bugs, skill uses, identifiers preserved) and
precision (redundant tool bodies and reasoning dropped), across the six evaluation dimensions. The
harness SHALL be usable to tune the summarization prompt and clearing policy and to detect quality
drift across re-compaction cycles.

#### Scenario: A recall probe detects a dropped identifier
- **WHEN** a summary omits a file path that was present in the compacted range
- **THEN** the recall probe flags the omission

#### Scenario: A precision probe flags retained bulk
- **WHEN** a summary reproduces verbatim a tool-result body that should have been condensed
- **THEN** the precision probe flags the redundancy

### Requirement: Summarization preserves spilled-result file paths

The summarizer-input scrub SHALL treat a tool result that carries a spilled-artifact path (an
envelope produced by the file-reference spill mode) as a path-bearing result, analogous to a
file-producing tool. When condensing such a result, the scrub SHALL inspect the result content for a
path matching the session-scoped spill shape (`sessions/<session_id>/tool_results/…`), preserve that
path verbatim in the stub, and drop the envelope preview/body. This extends the existing
durable-pointer preservation so that large results offloaded to session-scoped files remain
recallable by path after compaction, rather than being reduced to a generic `[cleared <tool>]` stub
that discards the path.

#### Scenario: A spilled result is summarized by its spill path

- **WHEN** the compacted range contains a tool result whose envelope references a file under `sessions/<session_id>/tool_results/`
- **THEN** the summarizer input references the spill path verbatim and does not include the envelope preview or the spilled file's content

#### Scenario: The agent can recall a spilled artifact after compaction

- **WHEN** a spilled result has been compacted to a path-preserving stub and the agent calls `read_file` with the preserved spill path on a later turn
- **THEN** the spilled file's contents are returned

#### Scenario: A non-spilled result of the same tool is still cleared generically

- **WHEN** the compacted range contains a `web_fetch` result that is not a spilled envelope (no tool_results path)
- **THEN** the result is condensed to the generic `[cleared web_fetch]` stub as before

### Requirement: Summary message shape is valid and enforced
A summary turn row's message SHALL be a single `role: user` message containing only `user_input_text` content blocks. The agent layer SHALL validate this shape before persisting the summary, and SHALL sanitize any loaded summary message by stripping role-invalid content blocks before injecting it into the replay list, so a malformed or legacy summary degrades gracefully instead of failing the model call. Compaction SHALL NOT attach a block whose type is invalid for the summary message's role (for example, it SHALL NOT append `assistant_gen_text` onto a `role: user` summary message); supplementary notes such as recently-accessed files SHALL be attached as `user_input_text`.

#### Scenario: A valid summary is persisted and replayed
- **WHEN** compaction persists a summary whose message is `role: user` with only `user_input_text` blocks
- **THEN** persistence accepts it and the next turn's replay injects it without error

#### Scenario: A recency note uses a role-valid block type
- **WHEN** compaction attaches a "recently accessed files" note to the summary message
- **THEN** the note is a `user_input_text` block, never `assistant_gen_text`

#### Scenario: A legacy malformed summary is sanitized on replay
- **WHEN** a loaded summary row carries an `assistant_gen_text` block on a `role: user` message
- **THEN** replay strips the invalid block and proceeds without a provider conversion error

### Requirement: A summary row's answer holds the summary text
Because a summary message carries no assistant block, the denormalized `answer` for a summary turn row SHALL be derived from the summary message's `user_input_text` block(s), so the row's `answer` is full-text searchable via `conversation_messages_fts`. Non-summary turns continue to derive `answer` from the last assistant block text.

#### Scenario: A summary row is full-text searchable by its content
- **WHEN** a summary turn row is persisted whose summary discusses a specific topic
- **THEN** the row's `answer` contains that summary text and a full-text search for a distinctive summary phrase matches the summary row

