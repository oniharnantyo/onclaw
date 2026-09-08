## MODIFIED Requirements

### Requirement: Non-blocking event tap
The live event stream SHALL behave as a tap supporting multiple concurrent subscriber consumers per active run, not a 1:1 dedicated pipeline: when no consumer drains a tap, or when a slow consumer leaves its buffer full, the runner SHALL drop further tap events for that subscriber rather than block. When a new consumer connects or re-connects to an in-flight run, the system SHALL attach a fresh live subscriber tap to the executing run. Persisted history SHALL remain complete regardless of tap drops or consumer disconnections, and a consumer MAY recover dropped or missed events from history by cursor (?after=).

#### Scenario: Unwatched run does not stall
- **WHEN** a run's events exceed the tap buffer and no consumer is reading
- **THEN** excess tap events are dropped, and the run itself proceeds and persists normally

#### Scenario: History recovers dropped tap events
- **WHEN** a consumer that missed tap events polls history with the last event it saw
- **THEN** it receives every persisted event after that cursor, including any the tap dropped

#### Scenario: Multiple concurrent consumers receive live stream
- **WHEN** a second client or reconnected stream subscribes to an active executing run
- **THEN** both subscribers receive subsequent live transcript events as they occur without stalling the execution loop

## ADDED Requirements

### Requirement: Streaming session event catch-up and live switchover
The session events endpoint SHALL support server-sent event (SSE) streaming with cursor pagination (`after`). When a client requests streaming events for a session, the endpoint SHALL first stream all persisted events occurring after the given cursor in chronological order. If a live run is currently active for that session, the endpoint SHALL seamlessly attach to the live run's event broadcast and stream subsequent events until the run reaches a terminal event (completed, error, or cancelled) and emit a terminal `[DONE]` marker. If no run is active, the endpoint SHALL close after replaying the persisted history.

#### Scenario: Catch-up on finished run
- **WHEN** a client connects with an `after` cursor to a session whose run is already completed
- **THEN** the server streams all persisted events after the cursor and immediately terminates the stream with a completed state

#### Scenario: Re-attach and live switchover on active run
- **WHEN** a client connects with an `after` cursor to a session while an agent execution is actively running
- **THEN** the server streams historical events past the cursor, transitions to streaming live execution events as they are produced, and terminates when the active run completes
