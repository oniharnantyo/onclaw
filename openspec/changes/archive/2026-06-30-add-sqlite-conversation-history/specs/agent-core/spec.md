## MODIFIED Requirements

### Requirement: The agent stays within the configured context budget

The system SHALL keep the conversation within `max_context_tokens` (default 8192) by
summarizing/compacting history before it exceeds the limit. The summarization SHALL trigger
well below the hard limit (around ~6000 tokens) so a turn never overruns the budget. Compaction
SHALL be durable across runs: the summary and a coverage cursor SHALL be persisted so that
subsequent replays stay bounded (cross-ref `conversation-history`).

#### Scenario: A long conversation is compacted

- **WHEN** accumulated history approaches the trigger threshold
- **THEN** older messages are summarized and recent messages are retained, the summary and coverage cursor are persisted, and the turn completes without exceeding `max_context_tokens`

### Requirement: Cancellation exits cleanly without a torn turn

The system SHALL propagate a cancellation signal (Ctrl-C, `/stop`, or a cancelled context) into
the running turn. On cancellation the loop SHALL stop promptly and return without panicking or
leaving a half-written turn. A partial assistant message produced mid-stream SHALL NOT be
persisted as a complete message. Cancellation is a control-flow condition and SHALL NOT be
recorded as a persisted message row.

#### Scenario: Ctrl-C mid-stream is clean

- **WHEN** the user interrupts a streaming turn
- **THEN** the turn stops and the process exits cleanly with no partial assistant message presented or persisted as complete

## REMOVED Requirements

### Requirement: The agent records a minimal append-only transcript

**Reason**: Replaced by the SQLite-backed `conversation-history` capability, which persists full
`*schema.Message` rows (not event-typed JSONL entries) via a history middleware. Recording
history is no longer the run loop's responsibility and the system no longer writes per-session
`.jsonl` files. The `interrupted` and `error` event markers are intentionally dropped — they were
control-flow signals, not messages; real messages remain fully captured.

**Migration**: Existing `conversations/<agent>_transcript.jsonl` files are not auto-imported.
History going forward lives in the SQLite database (`conversations` and `conversation_messages`
tables). Inspect it via the database directly; a future `onclaw history` command may surface it.