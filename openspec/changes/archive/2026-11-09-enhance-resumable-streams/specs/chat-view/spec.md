# Spec Delta: chat-view

## MODIFIED Requirements

### Requirement: Session re-entry keeps a running turn visible

When the user opens a session whose turn is in flight — including one paused
on a pending approval — the chat view SHALL reflect the in-flight turn: the
send control SHALL be the stop control, the session's turn-activity state
SHALL match the backend's, and the turn's terminal outcome SHALL settle the
view (stop returns to send, the rail's running indicator retires, and any
queued messages proceed). Reconstructing this state MUST consult the
backend's turn state, not only the frontend's own dispatch lifecycle, so a
turn that started before the session was opened is tracked exactly like one
the user dispatched. The view SHALL attach the session's live event feed
before the backend registration completes, so every event the backend
emits for the new subscription — the subscribe-time reconstruction
included — reaches the view rather than being dispatched before any
listener exists.

Re-entering a session mid-turn SHALL reconstruct the turn's in-flight state
in the transcript: partial output streamed before re-entry appears and
continues streaming, the in-flight call's reasoning appears as the
reasoning streamed so far, a tool call that is running appears as a running
row that the tool's terminal record later finalizes, and a delegated task
that is running appears as a running row that the task's terminal record
later finalizes. Reconstructed in-flight state MUST NOT duplicate or
displace the durable transcript: a reloaded transcript shows only the
durable events, and a message that completes after re-entry appears exactly
once.

Switching to a session MUST leave that session's live event feed working:
events emitted after the switch continue to update its transcript, its
running indicator, and its turn lifecycle without requiring another switch.

While the open session is busy, the view SHALL watch the stream's liveness
beacon: when a beacon reports more deltas emitted than the view has
received, or no beacon has arrived within the staleness window, or the
window regains focus while the turn is still in flight after a possible
interruption, the view SHALL request the backend resync and reconcile the
delivered in-flight state by replacing the reconstructed streaming entries
— the streamed text and reasoning — rather than appending to them, so the
stream continues exactly from the producer's position with nothing missing
and nothing doubled. Resync-triggered reconstruction obeys the same
exactly-once durable rules as re-entry reconstruction.

#### Scenario: Stop control after switching back

- **WHEN** the user switches to a session whose turn is in flight
- **THEN** the composer shows the stop control, activating it cancels the
  running turn, and the view settles on the turn's cancelled outcome

#### Scenario: Sends are not refused after switching back

- **WHEN** the user switches to a session whose turn is in flight and the
  turn reaches a terminal outcome
- **THEN** the composer returns to its send state and a new message sends
  without a turn-in-progress refusal

#### Scenario: Partial output reconstructed on re-entry

- **WHEN** the user opens a session while a model call is streaming
- **THEN** the text streamed so far is visible, continues streaming to
  completion, and the completed message appears once

#### Scenario: Subscribe-time reconstruction reaches the view

- **WHEN** the user opens a session while a model call is streaming and the
  backend emits the reconstruction for the new subscription
- **THEN** the streamed-so-far text is visible even though the backend
  emitted it as part of the subscription call, without requiring any
  further live delta to arrive first

#### Scenario: Reasoning reconstructed on re-entry

- **WHEN** the user opens a session while the model call is mid-reasoning
- **THEN** the reasoning streamed so far is visible and continues streaming
  to completion

#### Scenario: Running task reconstructed on re-entry

- **WHEN** the user opens a session while a delegated task is running
- **THEN** the transcript shows the task as running, and its terminal
  record finalizes that row when the task ends

#### Scenario: Running tool reconstructed on re-entry

- **WHEN** the user opens a session while a tool call is executing
- **THEN** the transcript shows the call as running, and its terminal
  record finalizes that row when the tool ends

#### Scenario: Live feed survives the switch

- **WHEN** the user switches to a session they had open before and events
  are emitted afterwards
- **THEN** the transcript continues to update live without the user
  switching away and back

#### Scenario: Resync heals a detected gap

- **WHEN** the open session is busy, a beacon reports more deltas emitted
  than the view has received, and the resync delivers the in-flight state
- **THEN** the streaming text is replaced with the producer's current
  partial — nothing missing, nothing doubled — and subsequent deltas
  continue from there

#### Scenario: Focus resyncs a busy session

- **WHEN** the window regains focus while the open session's turn is in
  flight
- **THEN** the view requests the resync and the reconstructed state matches
  the producer's current position

#### Scenario: Resync does not displace durable records

- **WHEN** a resync delivery lands in a transcript whose turn later completes
- **THEN** the completed message appears exactly once and no resync row
  survives the reload
