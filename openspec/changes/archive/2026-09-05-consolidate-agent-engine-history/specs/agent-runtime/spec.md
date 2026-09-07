## MODIFIED Requirements

### Requirement: Session history
Conversations SHALL be persisted as an append-only, totally ordered event log per thread, independent of any particular model message format. The log SHALL support: cursor pagination (after a given event id, with limit, optionally newest-first), filtering by event kind, and idempotent appends — appending an event whose identity already exists in the thread SHALL NOT duplicate it. A thread's current message window SHALL be reconstructible by replaying the log in order, including across summarization replacements. Execution checkpoints SHALL persist under a resolvable id. Every history query SHALL be workspace-scoped; events from one workspace SHALL be unreachable from another. A workspace member SHALL be able to read a session's persisted transcript as UI-shaped transcript events through the agents API, ordered by log sequence, with event-id cursor pagination; the read path SHALL NOT alter how the model receives context, which remains full per-session replay (no provider response-id chaining).

#### Scenario: Idempotent append
- **WHEN** the same event (identical thread and event identity) is persisted twice, e.g. after a persist retry
- **THEN** the thread contains exactly one copy — idempotency is guaranteed at the durable store layer, which ignores a conflicting re-insert (Postgres `ON CONFLICT DO NOTHING`); the ADK session adapter additionally rejects a cross-call duplicate EventID with `adk.ErrDuplicateEventID` so a runner retry of an already-committed event is surfaced rather than silently double-applied

#### Scenario: Cursor pagination newest-first
- **WHEN** a client pages a long thread backwards from the end with a limit
- **THEN** each page returns the newest events after the cursor position, newest-first, with the next cursor to continue

#### Scenario: Replay across compaction
- **WHEN** a thread's current window is reconstructed after a summarization replacement
- **THEN** replay yields the replaced (compressed) window as current, and the pre-compaction messages remain present in the log

#### Scenario: Cross-tenant history unreachable
- **WHEN** a member of workspace A addresses a thread belonging to workspace B
- **THEN** the history is not found, indistinguishable from an unknown thread

#### Scenario: Transcript read returns UI-shaped events
- **WHEN** a workspace member GETs a session's events endpoint for a thread with persisted messages and tool calls
- **THEN** the response contains transcript events in log order — user and assistant messages as completed-message events, tool calls as started/finished events, compaction as a context-compacted event — each carrying its turn id and occurrence time, plus a next-cursor when more pages remain

#### Scenario: Transcript read is cursor-paginated
- **WHEN** the read endpoint is called with `after` set to an event id and a `limit`
- **THEN** only events after that id are returned in a single page with the next cursor, and an unknown or out-of-range cursor yields an empty page rather than an error

#### Scenario: Read path leaves model context alone
- **WHEN** a subsequent chat turn executes on a thread after any number of transcript reads
- **THEN** the model still receives the full replayed session history from the event log, and no provider response-id chaining is introduced
