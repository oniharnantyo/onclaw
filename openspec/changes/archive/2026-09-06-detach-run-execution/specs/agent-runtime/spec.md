# agent-runtime Delta

## MODIFIED Requirements

### Requirement: Cancellation at a safe point
A running execution SHALL be cancellable by an explicit cancel request addressing the run (workspace, agent, session). Cancellation SHALL take effect at a safe point — the in-flight model call or tool call either completes and is recorded, or is aborted with its partial state marked — and SHALL record a cancel marker in the session history. Cancellation SHALL NOT leave dangling tool-call records that would break the next execution on the thread. A consumer disconnecting — an HTTP request returning, a stream client closing, or a stream never being consumed — SHALL NOT cancel the run.

#### Scenario: Cancel between tool calls
- **WHEN** a caller cancels while tool calls are executing
- **THEN** in-flight calls finish or abort cleanly, a cancel marker is recorded, and a subsequent execution on the thread starts from a consistent history

#### Scenario: Consumer disconnect does not cancel
- **WHEN** the client that started a turn closes its connection before the turn completes
- **THEN** the run continues to completion and its events are persisted to session history

#### Scenario: Explicit cancel endpoint
- **WHEN** an authorized member cancels the active run of a session
- **THEN** the run stops at the next safe point with a cancel marker, and the cancel endpoint reports the run as cancelled

## ADDED Requirements

### Requirement: Detached execution lifetime
An execution's context SHALL derive from the server's base context, not from any request or stream context. Completing the request that started a run, or ending any consumer's connection, SHALL NOT terminate or pause the run. The server SHALL track live runs so they can be cancelled explicitly and drained on graceful shutdown.

#### Scenario: Approval resume outlives its request
- **WHEN** an approval-resolution request returns its response before the resumed turn completes
- **THEN** the resumed turn continues running and its events reach session history

#### Scenario: Fire-and-forget start
- **WHEN** a caller starts a run and abandons the returned stream without consuming it
- **THEN** the run completes and persists its transcript

#### Scenario: Graceful shutdown drains runs
- **WHEN** the server shuts down while runs are in flight
- **THEN** in-flight runs get a bounded window to reach a terminal state, and any run that does not drain is cancelled with a cancel marker

### Requirement: Non-blocking event tap
The live event stream SHALL behave as a tap, not a pipeline: when no consumer drains it, or when a slow consumer leaves the buffer full, the runner SHALL drop further tap events rather than block. Persisted history SHALL remain complete regardless of tap drops, and a consumer MAY recover dropped events from history by cursor afterwards.

#### Scenario: Unwatched run does not stall
- **WHEN** a run's events exceed the tap buffer and no consumer is reading
- **THEN** excess tap events are dropped, and the run itself proceeds and persists normally

#### Scenario: History recovers dropped tap events
- **WHEN** a consumer that missed tap events polls history with the last event it saw
- **THEN** it receives every persisted event after that cursor, including any the tap dropped
