# Spec Delta

## MODIFIED Requirements

### Requirement: Chat transcript rendering

The chat view SHALL render user messages, assistant messages, reasoning bubbles, tool-call cards (whose anatomy, states, bodies, and folding are specified by `chat-tool-cards`), and compaction markers in chronological order. Streaming output MUST be appended live during a turn.

Assistant message text SHALL render as GitHub-flavored markdown: pipe tables render as tables, task-list items render with their checked state, strikethrough and autolinked addresses render as such, and ordered lists render with their numbers. Raw markdown syntax MUST NOT be shown for any of these constructs once the message has completed.

While a turn is in flight the transcript SHALL show a turn-activity indicator at the tail of the running assistant message: a typing indicator before the first token arrives, and once a tool is running, a live status line naming the running tool and its elapsed time. The indicator SHALL disappear when the turn reaches a terminal outcome and MUST honor reduced motion by rendering as static text.

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

### Requirement: Day dividers

The transcript SHALL insert a day divider between consecutive dated messages of different calendar days, labeled Today, Yesterday, or the date, with the full timestamp available on the divider. No divider SHALL appear before the first dated message. Each dated message SHALL reveal its own time of day on hover or focus, without the time occupying space when the message is not hovered.

#### Scenario: Divider between days

- **WHEN** the transcript contains messages from yesterday and today
- **THEN** a labeled divider separates them at the day boundary

#### Scenario: No leading divider

- **WHEN** the transcript is viewed from its first message on a single day
- **THEN** no day divider appears above the first message

#### Scenario: Hover reveals a message time

- **WHEN** the user hovers a dated message in the transcript
- **THEN** that message's time of day is revealed beside it and hides again when the pointer leaves

### Requirement: Conversation minimap

The chat view SHALL render a conversation map beside the transcript on wide widths: one tick per turn in the rendered window, with the tick for the turn being read marked, ticks for turns currently on screen deepened, a hover preview naming the turn, and clicking a tick scrolling to its turn. The map SHALL hide when fewer than two turns are rendered and at narrow widths, and MUST NOT appear in Settings.

#### Scenario: Tick navigates to a message

- **WHEN** the user clicks a tick in the conversation map
- **THEN** the transcript scrolls to the corresponding turn's first message

#### Scenario: Map tracks the reading position

- **WHEN** the user scrolls the transcript so a different turn fills the viewport
- **THEN** that turn's tick becomes the marked one and the ticks of on-screen turns are deepened

#### Scenario: Hover previews the turn

- **WHEN** the user hovers a tick
- **THEN** a preview naming the turn appears beside the tick

#### Scenario: Minimap hides when sparse

- **WHEN** the session has fewer than two rendered turns
- **THEN** no conversation map is shown

### Requirement: Code block presentation

Code blocks in assistant messages and in tool-card previews SHALL present a
header with the language label when known, a copy control, and a wrap
toggle. Code SHALL be syntax highlighted once its message has finished
streaming and SHALL render as plain monospace text while streaming. Both the
plain and highlighted forms SHALL be legible in the light and dark themes.

A fenced block whose language is `mermaid` SHALL render as a diagram once its message has finished streaming and SHALL show its source as plain text while streaming. When the diagram source does not parse, the block SHALL show the source with a quiet failure note rather than an empty or broken area. A rendered diagram SHALL re-render to match the active theme when the theme changes. Diagram rendering MUST NOT load its rendering code until the first diagram is shown.

#### Scenario: Completed message highlights its code

- **WHEN** an assistant message containing a fenced code block finishes
  streaming
- **THEN** the block renders with syntax highlighting, its language label, and
  its copy control

#### Scenario: Streaming code stays plain

- **WHEN** a fenced code block is still arriving
- **THEN** it renders as plain monospace text without highlighting

#### Scenario: Mermaid block renders as a diagram

- **WHEN** an assistant message containing a fenced `mermaid` block finishes streaming
- **THEN** the block renders the diagram in place of the source

#### Scenario: Mermaid source while streaming

- **WHEN** a fenced `mermaid` block is still arriving
- **THEN** the block shows the source as plain text and no diagram is attempted

#### Scenario: Unparseable diagram falls back

- **WHEN** a completed `mermaid` block's source does not parse
- **THEN** the block shows the source text with a quiet failure note and the rest of the message renders normally

#### Scenario: Diagram follows the theme

- **WHEN** the user switches theme while a rendered diagram is visible
- **THEN** the diagram re-renders in the new theme's colors
