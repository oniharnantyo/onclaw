## ADDED Requirements

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