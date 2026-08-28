# web-app/chat Delta — adopt-assistant-ui-chat

## MODIFIED Requirements

### Requirement: Message send and simulated response
Sending a message SHALL append it to the active session, derive the session title from the first user message (truncated at 42 characters), and produce the agent's reply through the chat runtime bridge. The reply SHALL stream into the transcript incrementally, with a running indicator while the turn is in flight. The reply SHALL include a tool-call card when the responding agent has tools granted. While a turn is in flight the send control SHALL become a stop control that cancels the turn; partial text SHALL remain in the transcript. Teammate direct messages SHALL NOT trigger an agent turn.

#### Scenario: Direct agent chat
- **WHEN** the user sends a message to agent "Atlas"
- **THEN** a running indicator appears, then Atlas's reply streams in incrementally

#### Scenario: Teammate direct message
- **WHEN** the user sends a message in a teammate DM
- **THEN** the message is appended and no agent turn occurs

#### Scenario: Stop control
- **WHEN** the user presses stop while Atlas is responding
- **THEN** the turn cancels, the running indicator clears, and partial text remains

### Requirement: Branching and regeneration
Regenerating an agent message SHALL append a new variant to that message and switch to it, navigating variants through the runtime's branch state with an `n / total` indicator. Editing a previously sent user message SHALL replace its text, drop every later message in the session, and re-run the agent on the new text. Regenerate and edit affordances SHALL be available in agent direct chats only; channel agent messages SHALL NOT offer regenerate.

#### Scenario: Regenerate
- **WHEN** the user triggers regenerate on the latest agent reply in an agent DM
- **THEN** a new variant appears and the picker shows `2 / 2`

#### Scenario: Edit and resubmit
- **WHEN** the user edits an earlier user message in an agent DM and submits
- **THEN** the session is truncated at that message and a new agent response is generated

#### Scenario: No regenerate in channels
- **WHEN** a channel agent message renders
- **THEN** no regenerate control is offered

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity, and agent tool invocations rendered as expandable cards exposing tool name, arguments, and latency in milliseconds. A tool card SHALL show a running state while its turn is in flight and latency once completed. Messages produced by a scheduled run SHALL carry a visible cron-origin marker naming the schedule.

#### Scenario: Tool call card
- **WHEN** an agent message includes a tool invocation that has completed
- **THEN** the transcript shows a card with the tool name, its arguments, and the latency (e.g. `760ms`)

#### Scenario: Tool card running state
- **WHEN** an agent message's tool invocation is still executing
- **THEN** the card shows a running state instead of a latency value

#### Scenario: Cron-origin message
- **WHEN** a thread message was produced by schedule "morning-digest"
- **THEN** the message displays a marker naming that schedule
