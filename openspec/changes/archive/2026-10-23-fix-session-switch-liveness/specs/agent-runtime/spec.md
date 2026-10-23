## ADDED Requirements

### Requirement: In-flight turn state query

The runtime SHALL expose, per session, whether a turn is in flight and the
in-flight turn's identifier, through a bound call the chat UI can consult. A
turn parked on a pending approval SHALL report as in flight. A session with
no turn in flight SHALL report not busy; reporting state for an unknown
session SHALL be safe and read as not busy.

#### Scenario: Query during a turn

- **WHEN** the turn state of a session with a running turn is queried
- **THEN** the answer reports the session busy and carries that turn's
  identifier

#### Scenario: Approval-parked turn reports in flight

- **WHEN** the turn state of a session parked on a pending approval is
  queried
- **THEN** the answer reports the session busy with the parked turn's
  identifier

#### Scenario: Idle session reports not busy

- **WHEN** the turn state of a session with no turn in flight is queried
- **THEN** the answer reports the session not busy

### Requirement: Subscribe-time live-state catch-up

When a session's live event stream is subscribed while one of its turns is
in flight, the runtime SHALL deliver the turn's in-flight state to the new
subscriber before any subsequent live events: the model call's partial
output streamed so far and the tool calls currently executing, announced
with their identifiers and arguments. These catch-up events are live-only —
MUST NOT be persisted to the transcript — and a reloaded transcript MUST
show only the durable events. Catch-up state MUST reconcile with the
turn's own records: a running tool's terminal record finalizes the
reconstructed row, and the turn's durable assistant message lands exactly
once. Subscribing to a session with no turn in flight SHALL deliver no
catch-up events.

#### Scenario: Late subscriber sees streamed-so-far text

- **WHEN** a session is subscribed while a model call is mid-stream
- **THEN** the new subscriber receives the partial output streamed so far
  before the next live delta, and the turn's terminal transcript is not
  duplicated by the reconstruction

#### Scenario: Late subscriber sees the running tool

- **WHEN** a session is subscribed while a tool call is executing
- **THEN** the new subscriber receives that call as a running announcement
  carrying its identifier, and the call's terminal record later finalizes it

#### Scenario: Catch-up is not persisted

- **WHEN** a session that received catch-up events is reloaded later
- **THEN** the transcript contains only the durable events — no
  reconstruction rows

#### Scenario: Subscribe to an idle session

- **WHEN** a session with no turn in flight is subscribed
- **THEN** no catch-up events are delivered and the stream behaves as before
