## ADDED Requirements

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
