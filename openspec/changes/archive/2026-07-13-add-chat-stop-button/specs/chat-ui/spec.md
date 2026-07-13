## ADDED Requirements

### Requirement: Stop Control During Streaming
The composer SHALL surface a stop control that is visible and enabled only while the agent is streaming, and that cancels the in-flight stream when activated. The send control and the stop control SHALL be mutually exclusive by streaming state (send while idle, stop while streaming), rendered in the same composer action slot.

#### Scenario: A stop control appears while streaming
- **WHEN** the agent begins streaming a response
- **THEN** the composer action slot renders an enabled stop control in place of the send control

#### Scenario: The send control returns when streaming ends
- **WHEN** streaming ends for any reason (completion, stop, or error)
- **THEN** the composer action slot renders the send control again

#### Scenario: Activating stop cancels the stream
- **WHEN** the user activates the stop control during streaming
- **THEN** the in-flight chat stream is cancelled and no further streaming content is processed for that turn

### Requirement: Abort-Based Stream Cancellation
The chat runtime SHALL cancel an in-flight stream by aborting the underlying `/api/chat` request via an `AbortController` signal. Cancellation SHALL be signalled to the backend solely by closing the connection (which cancels the request context the agent runs under); no separate stop endpoint SHALL be introduced. A user-initiated abort SHALL be distinguished from a genuine network error so that it is treated as a normal end of the turn, not an error.

#### Scenario: Stop aborts the fetch, not a new request
- **WHEN** the user stops a running stream
- **THEN** the runtime aborts the active `/api/chat` fetch via its `AbortController` rather than issuing a second request

#### Scenario: Abort is not reported as an error
- **WHEN** the fetch is aborted by the stop control
- **THEN** no error toast is shown and the stream is treated as normally ended (the streaming flag is cleared)

#### Scenario: A real network error is still reported
- **WHEN** the fetch fails for a reason other than a user abort
- **THEN** the existing stream-error toast and error state are shown

### Requirement: Stopped-Partial Retention In Memory
When the user stops a stream, the partial assistant content already received SHALL remain visible in the transcript for the remainder of the session and SHALL be marked as stopped. The runtime SHALL NOT re-fetch conversation history from the server as a consequence of a stop (which would otherwise replace the in-memory partial with persisted history, dropping the partial because a stopped turn is not persisted). A stopped partial SHALL NOT be persisted to the conversation history.

#### Scenario: The partial stays visible after stop
- **WHEN** the user stops a stream mid-response
- **THEN** the assistant text received so far remains in the transcript, marked as stopped, without an immediate history re-fetch

#### Scenario: The stopped partial is visually marked
- **WHEN** a stopped assistant message renders
- **THEN** it is visually distinguished from completed messages via a "stopped" affordance

#### Scenario: A stopped turn is not persisted
- **WHEN** a turn is stopped and the conversation is later reloaded from the server
- **THEN** that stopped turn is absent from the persisted history (the partial existed only in the prior session's memory)
