# conversation-history

## Purpose

Persist full conversation messages (the entire `*schema.Message`) to the SQLite
database onclaw already opens, replay them into the agent before each run for
multi-turn memory, and keep replay bounded via durable summarization compaction.
History replaces the former per-session `.jsonl` transcript and is the substrate
for future durable summarization and vector search.
## Requirements
### Requirement: Conversation history is persisted as full messages in SQLite

The system SHALL persist every conversation message (system, user, assistant, tool) to the
SQLite database as an immutable row carrying the full `*schema.AgenticMessage` (role, content
blocks — including assistant text, reasoning, function tool calls, and function tool results —
response metadata, and the message `Extra` map). Messages SHALL be grouped into conversations;
each conversation SHALL belong to an agent and each message SHALL carry a monotonically
increasing per-conversation `seq` assigned at insert. Persistence SHALL be append-only; the run
loop SHALL NOT mutate or delete existing rows. The store package SHALL remain free of eino
imports; the agent layer SHALL perform `*schema.AgenticMessage` <-> JSON conversion and secret
redaction (walking the content blocks) before persistence. **BREAKING:** rows previously
persisted as `*schema.Message` JSON are not read back; the conversation-history feature is
newly shipped, so this is a clean format break with no read-side legacy shim.

#### Scenario: A turn is persisted as full agentic messages

- **WHEN** a turn runs that calls a tool and returns an answer
- **THEN** the database holds user, assistant (with tool-call content blocks), tool-result, and final-assistant message rows for that conversation, ordered by `seq`

#### Scenario: Tool calls are stored within the assistant message

- **WHEN** the assistant emits a message that requests a tool call
- **THEN** the tool call is stored inside that assistant message's content blocks rather than as a separate row

### Requirement: History is replayed into the agent before each run

The system SHALL, before each agent run, load persisted history for the active conversation and
inject it before the new user message so the agent has multi-turn memory. Replay SHALL load only
the bounded working set — the latest summary (if any) plus the messages after the summary's
coverage cursor — and SHALL NOT load the full raw session into memory. `onclaw chat` SHALL reuse
one conversation across all turns of a REPL session; `onclaw run` SHALL create one conversation
per invocation.

#### Scenario: A REPL session remembers prior turns

- **WHEN** a user runs `onclaw chat`, asks something in turn 1, and refers to it in turn 2
- **THEN** the agent answers using the history injected from the shared conversation

#### Scenario: Replay is bounded after compaction

- **WHEN** a conversation has been compacted and a new turn begins
- **THEN** only the latest summary and the messages after its coverage cursor are injected, not the full raw history

### Requirement: New messages are saved eagerly as they are produced

The system SHALL save each new message to the conversation store as soon as it is produced by
the agent, via a history middleware — not the run loop. The new user message SHALL be saved at
the start of the run, before any compaction can occur. Saving SHALL be robust to in-run
summarization compaction by tracking which messages have already been persisted individually
rather than by positional index, so messages are never lost or duplicated when compaction
rewrites the in-memory state.

#### Scenario: A crash mid-turn loses at most the message being produced

- **WHEN** a turn is interrupted after the assistant has produced one message but before the next
- **THEN** the user message and the completed assistant message are already persisted; only the in-flight message is absent

### Requirement: Summarization compaction is durable across runs

The system SHALL persist the summary and a coverage cursor whenever in-run summarization
compacts history, so compacted messages are represented by the summary on subsequent replays.
The coverage cursor SHALL record the highest message `seq` the summary covers. Replay SHALL
inject the summary followed by messages with `seq` beyond the cursor; compacted originals SHALL
remain in the database for audit but SHALL NOT be re-injected into the model.

#### Scenario: Compaction persists and is reused on replay

- **WHEN** a long conversation exceeds the summarization threshold and compaction fires
- **THEN** a summary row is persisted, the coverage cursor advances, and the next turn replays the summary plus the tail instead of the full history

### Requirement: Persisted history excludes resolved secret values

The system SHALL redact known secret values from message content, tool-call arguments, and tool
results before persisting them, using the same redaction applied to transcripts today
(cross-ref `providers`). The conversation store SHALL NOT receive or hold resolved secret
values.

#### Scenario: A message containing a secret is redacted

- **WHEN** a tool result contains a resolved secret value
- **THEN** the persisted row contains the redacted form, not the secret

### Requirement: Conversations can be enumerated

The system SHALL provide `ConversationStore.ListConversations(ctx) ([]*ConversationRow, error)` returning each conversation's id, agent name, created and updated timestamps, and message count. The store interface, the `ConversationRow` DTO, and the SQLite implementation SHALL follow the existing contract/types/implementation separation. This enumeration supports the web UI's conversation list and any future listing surface.

#### Scenario: conversations are listed with counts

- **WHEN** a conversation with several messages exists and `ListConversations` is called
- **THEN** the result includes that conversation's id, agent name, timestamps, and the correct message count

#### Scenario: an empty store lists nothing

- **WHEN** `ListConversations` is called and no conversations exist
- **THEN** it returns an empty (or nil) slice and no error

### Requirement: Conversation history is full-text searchable

The conversation store SHALL support FTS5 search over stored messages so the agent can recall
specific past conversations via `session_search`, independent of the live history window.

#### Scenario: A past message is found by keyword

- **WHEN** `session_search` runs a query that matches a message from a prior session
- **THEN** that message is returned ranked by FTS5 relevance

### Requirement: Memory flush precedes summary persistence

The summarization path SHALL offer a flush hook that runs memory extraction over the messages
being compacted before the compaction summary is persisted to the conversation store.

#### Scenario: The flush hook runs before SaveSummary

- **WHEN** the summarization middleware persists a compaction summary
- **THEN** the memory flush hook has already run over the compacted message range

