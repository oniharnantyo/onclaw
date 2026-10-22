# Spec Delta

## MODIFIED Requirements

### Requirement: Chat transcript rendering

The chat view SHALL render user messages, assistant messages, reasoning bubbles, tool-call cards (whose anatomy, states, bodies, and folding are specified by `chat-tool-cards`), and compaction markers in chronological order. Streaming output MUST be appended live during a turn.

Assistant message text SHALL render as GitHub-flavored markdown: pipe tables render as tables, task-list items render with their checked state, strikethrough and autolinked addresses render as such, and ordered lists render with their numbers. Raw markdown syntax MUST NOT be shown for any of these constructs once the message has completed.

While a turn is in flight the transcript SHALL show a turn-activity indicator at the tail of the running assistant message: a typing indicator before the first token arrives, and once a tool is running, a live status line naming the running tool and its elapsed time. The indicator SHALL disappear when the turn reaches a terminal outcome and MUST honor reduced motion by rendering as static text.

The transcript SHALL present the agent's activity and its answer as one flat reading surface rather than as nested cards. Reasoning SHALL render as a single quiet disclosure line — a brain glyph, the word "Reasoning", the elapsed time when the group carries one, and a disclosure control — that opens into an indented aside with a bounded scroll region; a reasoning group MUST NOT be enclosed in a bordered or filled surface of its own. A run of fewer than four consecutive tool calls SHALL render each call as its own line in the transcript flow, in message order, and MUST NOT be collapsed behind a group disclosure. Every assistant turn SHALL be separated from the one before it by a horizontal rule, and the assistant's answer and the rows above it SHALL share one left edge and one column measure.

The transcript SHALL follow its live tail: while the reader is at the bottom, newly appended entries and streaming text keep the transcript pinned to the bottom. A reader's own scroll away from the bottom SHALL pause following; following SHALL resume when the reader scrolls back to the bottom or activates a jump-to-latest control, which SHALL be visible only while following is paused. Programmatic scrolling MUST NOT change whether the reader is following. Sending a message SHALL bring the transcript to the bottom so the reply starts in view. Expanding or collapsing a card or bubble inside the transcript MUST NOT move the reader's viewport.

#### Scenario: Tool call visible in transcript

- **WHEN** an agent turn includes tool activity
- **THEN** the transcript shows a tool-call card in message order whose header names the tool and whose body carries its result

#### Scenario: Live streaming in transcript

- **WHEN** an agent is responding
- **THEN** partial text appears in the transcript as it streams

#### Scenario: Table renders as a table

- **WHEN** a completed assistant message contains a pipe-delimited markdown table
- **THEN** the transcript shows a table with header and body rows, not the raw pipe text

#### Scenario: Ordered list keeps its numbers

- **WHEN** a completed assistant message contains an ordered list
- **THEN** each item renders with its number in sequence

#### Scenario: Task list shows checked state

- **WHEN** a completed assistant message contains task-list items, some checked
- **THEN** each item renders with a checkbox reflecting its checked state

#### Scenario: Typing indicator before the first token

- **WHEN** the user has sent a message and the agent has not yet produced any output
- **THEN** the transcript shows a typing indicator where the reply will appear

#### Scenario: Status line names the running tool

- **WHEN** a tool call is running during a turn
- **THEN** the transcript shows a status line naming that tool with a live elapsed time, which disappears when the turn ends

#### Scenario: Reasoning renders as a disclosure line

- **WHEN** a turn contains reasoning
- **THEN** the transcript shows the reasoning as one muted line carrying a brain glyph, the word "Reasoning", and a disclosure control, with no bordered surface around it, and activating the control reveals the reasoning indented beneath the line

#### Scenario: Short tool run renders inline

- **WHEN** a turn contains fewer than four consecutive tool calls
- **THEN** each call appears in the transcript flow as its own line, in message order, with no group disclosure standing in for them

#### Scenario: Turns are separated by a rule

- **WHEN** the transcript renders more than one assistant turn
- **THEN** a horizontal rule separates each assistant turn from the one before it, and the answer and the rows above it share one left edge and one column measure

#### Scenario: Transcript follows a streaming reply

- **WHEN** the reader is at the bottom and the agent streams a reply longer than the viewport
- **THEN** the transcript stays pinned to the bottom as the text grows

#### Scenario: Scrolling up pauses following

- **WHEN** the reader scrolls up during a streaming reply
- **THEN** the viewport stays where the reader left it while the reply continues, and the jump-to-latest control appears

#### Scenario: Jump to latest resumes following

- **WHEN** the reader activates the jump-to-latest control
- **THEN** the transcript scrolls to the bottom, the control hides, and following resumes

#### Scenario: Own send returns to the bottom

- **WHEN** the reader sends a message while scrolled up in the transcript
- **THEN** the transcript moves to the bottom and follows the reply

#### Scenario: Expanding a card holds the viewport

- **WHEN** the reader expands a tool card above the fold during a streaming reply
- **THEN** the card opens in place without the viewport jumping to the bottom
