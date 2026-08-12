## ADDED Requirements

### Requirement: Episodic capture is gated by a signal filter

The system SHALL evaluate whether a session produced enough signal to warrant an episodic entry before invoking summarization. A session that already triggered compaction SHALL always pass the gate (the compaction summary is sufficient evidence of substance). A session without compaction SHALL pass only if the messages contain substantive content: tool calls, durable-signal keywords, or user messages exceeding trivial greeting length. A session consisting solely of short greetings (e.g., "Hello" / "Hi, how can I help?") SHALL NOT produce an episodic entry.

#### Scenario: A trivial greeting exchange is skipped

- **WHEN** a session ends with only 1-2 short messages containing no tool calls or durable signals
- **THEN** no episodic summary is created and no LLM summarization call is made

#### Scenario: A session with tool usage always captures an episode

- **WHEN** a session ends with messages containing at least one tool call
- **THEN** the signal gate passes and an episodic summary is created

#### Scenario: A compacted session always captures an episode

- **WHEN** a session ends and a compaction summary exists
- **THEN** the signal gate passes regardless of individual message content

#### Scenario: Explicit durable instructions are captured

- **WHEN** a session ends with user messages containing keywords like "remember", "prefer", or "always"
- **THEN** the signal gate passes and an episodic summary is created

### Requirement: Episodic background work is dispatched via the memory bus

After an episodic summary is written to the database, the system SHALL publish an `EpisodeCreated` event to the memory bus. Knowledge-graph entity extraction and dreaming consolidation SHALL be triggered by consuming this event asynchronously, not by inline synchronous calls in the session-end path.

#### Scenario: KG extraction runs asynchronously after episode creation

- **WHEN** an episodic summary is appended to the database
- **THEN** KG entity extraction runs in a background worker via the bus, not inline in `FlushMessages`

#### Scenario: Dreamer checks threshold asynchronously after episode creation

- **WHEN** an `EpisodeCreated` event is received by the Dreamer worker
- **THEN** `MaybeDream` executes in the background without blocking the session-end path

#### Scenario: Session-end returns before background work completes

- **WHEN** `FlushMessages` publishes an `EpisodeCreated` event
- **THEN** control returns to the caller immediately after the publish (not after KG extraction or dreaming)

## MODIFIED Requirements

### Requirement: Memory extraction flushes before compaction

The system SHALL extract durable facts from messages about to be removed by compaction before the
compaction summary is persisted. Extraction SHALL be idempotent across retries via a write-cursor
stored in message metadata. A session that ends without reaching the compaction threshold SHALL
still be flushed on session stop. The compaction summary text produced by the summarization middleware SHALL be stored on the memory middleware so that episodic summarization can reuse it without an additional LLM call.

#### Scenario: Facts are flushed before a compaction summary is saved

- **WHEN** the summarization middleware compacts a range of messages
- **THEN** durable facts from that range are written to memory before the summary is persisted

#### Scenario: A retried turn does not double-extract

- **WHEN** extraction runs again over a range already covered by the write-cursor
- **THEN** no duplicate memory is written

#### Scenario: Compaction summary is stored for episodic reuse

- **WHEN** the summarization middleware produces a compaction summary
- **THEN** the summary text is stored on the memory middleware's `CompactionSummary` field before `Finalize` returns

#### Scenario: A session that compacted reuses the summary for episodic capture

- **WHEN** a session ends after compaction has occurred
- **THEN** episodic summarization uses the stored compaction summary directly without making a new LLM call
