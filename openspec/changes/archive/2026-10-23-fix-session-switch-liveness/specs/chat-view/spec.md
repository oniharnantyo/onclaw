## ADDED Requirements

### Requirement: Session re-entry keeps a running turn visible

When the user opens a session whose turn is in flight — including one paused
on a pending approval — the chat view SHALL reflect the in-flight turn: the
send control SHALL be the stop control, the session's turn-activity state
SHALL match the backend's, and the turn's terminal outcome SHALL settle the
view (stop returns to send, the rail's running indicator retires, and any
queued messages proceed). Reconstructing this state MUST consult the
backend's turn state, not only the frontend's own dispatch lifecycle, so a
turn that started before the session was opened is tracked exactly like one
the user dispatched.

Re-entering a session mid-turn SHALL reconstruct the turn's in-flight state
in the transcript: partial output streamed before re-entry appears and
continues streaming, and a tool call that is running appears as a running
row that the tool's terminal record later finalizes. Reconstructed
in-flight state MUST NOT duplicate or displace the durable transcript: a
reloaded transcript shows only the durable events, and a message that
completes after re-entry appears exactly once.

Switching to a session MUST leave that session's live event feed working:
events emitted after the switch continue to update its transcript, its
running indicator, and its turn lifecycle without requiring another switch.

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

#### Scenario: Running tool reconstructed on re-entry

- **WHEN** the user opens a session while a tool call is executing
- **THEN** the transcript shows the call as running, and its terminal record
  finalizes that row when the tool ends

#### Scenario: Live feed survives the switch

- **WHEN** the user switches to a session they had open before and events
  are emitted afterwards
- **THEN** the transcript continues to update live without the user
  switching away and back
