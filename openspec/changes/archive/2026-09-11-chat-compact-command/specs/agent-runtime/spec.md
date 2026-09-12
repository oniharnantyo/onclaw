## ADDED Requirements

### Requirement: Manual compaction command
A turn submitted with the compact command SHALL NOT run a normal model chat turn and SHALL NOT append a user message to the session. The runtime SHALL load the session's current message window, generate a summary with the agent's own provider/model under an instruction incorporating the command's optional focus text, offload the full pre-compaction history to `transcript.md` in the agent's workspace directory (the same retention rule as automatic compaction), record the window replacement in the session history, and emit the context-compacted transcript event — carrying before/after working-context token estimates — followed by a terminal turn-completed event carrying the summarizer call's usage. Manual compaction SHALL execute regardless of the current token count. A compact command against a session with no compactable message history SHALL complete without emitting a compaction event.

#### Scenario: Compact rewrites the window
- **WHEN** a compact command executes on a session with prior turns
- **THEN** the working window is replaced by the summary, `transcript.md` in the agent directory holds the full prior history, and the stream ends with the context-compacted event (token estimates included) followed by turn-completed carrying the summarizer usage

#### Scenario: Focus text shapes the summary instruction
- **WHEN** the compact command carries focus text
- **THEN** the summary is generated under an instruction that incorporates that text

#### Scenario: Below-threshold compaction allowed
- **WHEN** a compact command executes on a session whose token count is under the automatic trigger threshold
- **THEN** compaction still executes

#### Scenario: Empty history is a quiet no-op
- **WHEN** a compact command executes on a session with no compactable messages
- **THEN** the turn completes without error and without a compaction event

## MODIFIED Requirements

### Requirement: Context summarization
When an execution's working-context token count exceeds the resolved context window multiplied by a server-configured safety margin, the runtime SHALL compress the conversation history into a summary generated with the agent's own provider/model, SHALL offload the full pre-compaction history to `transcript.md` inside the agent's workspace directory, SHALL continue the execution with the compressed window plus the agent's recent user messages, and SHALL record the replacement in the session history so the full prior record remains retrievable by replay. The context-compacted transcript event — emitted live and on replay of the session log — SHALL carry the working-context token estimates before and after the compaction.

#### Scenario: Trigger fires mid-conversation
- **WHEN** a long thread's token count crosses the resolved context window × margin
- **THEN** the next execution continues from a compressed window and `transcript.md` in the agent directory contains the full prior history

#### Scenario: Compaction is auditable
- **WHEN** a compaction has occurred on a thread
- **THEN** the session history contains a window-replacement record and replaying the full log still yields the pre-compaction messages

#### Scenario: Compaction event carries token estimates
- **WHEN** a context-compacted event is emitted live or replayed from the session log
- **THEN** it carries the working-context token estimates before and after the compaction
