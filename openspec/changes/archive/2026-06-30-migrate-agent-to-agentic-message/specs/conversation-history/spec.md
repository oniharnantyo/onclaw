## MODIFIED Requirements

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