## MODIFIED Requirements

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
