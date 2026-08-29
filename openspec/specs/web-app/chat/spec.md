# web-app/chat Specification

## Purpose

The chat experience for agents, channels, and teammate direct messages: transcript rendering with tool-call cards, the composer with slash-command and mention menus, per-agent session lists, and runtime-driven conversation behavior — streaming replies with stop/cancel, edit-and-resubmit, and branch navigation.

## Requirements

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

### Requirement: Slash commands
Typing `/` at the start of the composer SHALL open a filtered command menu (`/tools`, `/model`, `/schedule`, `/reset`, `/help`) navigable by arrow keys with Enter completing the command. `/tools`, `/model`, and `/help` SHALL produce scripted replies derived from the agent's actual configuration. `/reset` SHALL clear the active session without contacting the agent. `/schedule` SHALL reply and then open the schedule editor.

#### Scenario: Scripted reply
- **WHEN** the user sends `/model` to an agent running `claude-sonnet-5` at temperature 0.3
- **THEN** the reply states that model and temperature

#### Scenario: Reset
- **WHEN** the user sends `/reset`
- **THEN** the active session's messages are cleared and a confirmation toast appears

### Requirement: Channel mentions
In a channel, typing `@` SHALL open a menu of channel members filtered by handle prefix. A message mentioning one or more agents SHALL cause each mentioned agent to respond as itself, staggered in time; a message with no mentions SHALL fall to the channel's primary agent.

#### Scenario: Mentioned agent responds
- **WHEN** the user posts "@Warden can you check the budget?" in `#incidents`
- **THEN** Warden (not the channel's primary agent) replies in the thread

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

### Requirement: Per-agent session lists
Each agent chat SHALL support multiple named sessions. The sidebar SHALL list sessions for the active agent chat with controls to start a new session, switch sessions, and delete one. Deleting the last session SHALL spawn a fresh empty session, so a chat never has zero sessions.

#### Scenario: Session lifecycle
- **WHEN** the user deletes the only session of an agent chat
- **THEN** a new empty session titled "New chat" becomes active

### Requirement: Transcript windowing
Long transcripts SHALL render only the latest 80 messages initially, with a "Load earlier messages" control that grows the window by 400; a freshly sent user message SHALL scroll so that it pins to the top of the viewport, and a floating jump-to-bottom control SHALL appear whenever the transcript is scrolled away from the bottom.

#### Scenario: Load earlier
- **WHEN** a session holds 500 messages
- **THEN** the newest 80 render and the control reports the hidden count

### Requirement: Empty state
An empty session with an agent SHALL render the agent's identity, a one-line role description, and four clickable starter prompts that send on click.

#### Scenario: Starter prompt
- **WHEN** the user clicks a starter prompt in an empty session
- **THEN** it is sent as the user's message
