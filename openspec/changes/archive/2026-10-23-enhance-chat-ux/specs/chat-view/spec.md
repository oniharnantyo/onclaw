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

### Requirement: Transcript windowing

The transcript SHALL load its most recent window of history first and offer a "load earlier" affordance at the top that prepends older events in chronological order until the session's beginning is reached. Prepending MUST preserve the reader's scroll position, live streaming MUST NOT disturb a reader who has scrolled away from the bottom, and a page boundary falling inside a turn SHALL present that turn as one continuous entry.

#### Scenario: Load earlier keeps reading position

- **WHEN** the user activates "load earlier" in a scrolled transcript
- **THEN** older entries appear above and the visible message under the cursor stays in place

#### Scenario: Window boundary inside a turn

- **WHEN** an earlier page would end in the middle of a turn's messages
- **THEN** the turn renders as a single unbroken entry after loading

### Requirement: Sessions organized per agent

The chat menu SHALL list agents and, for each agent, its sessions with latest
activity. The session layer SHALL render as the content island's left column
— separated from the conversation by a hairline border, between the
navigation rail and the conversation — with a header row ("Sessions" plus a
new-session button) and compact session entries showing the session title.
The active session SHALL be highlighted with the primary tint. The user SHALL
be able to start a new session for an agent and switch between sessions
without losing history. The column SHALL be hidden while Settings is open.

The unfiltered list SHALL show the four most recent sessions — plus the
active session when it is older than those — behind a control that reveals
the remaining older sessions and a control that returns to the recent view;
a non-empty search SHALL show every match regardless of the cap.

Each session entry SHALL offer a delete affordance that removes the session
and its transcript only after an explicit confirmation; deleting MUST NOT
affect the agent's other sessions, and the backend MUST refuse the deletion
of a session with a turn in flight. An entry whose session has a turn in
flight SHALL show a running indicator: an inline spinner before the title
with the title pulsing, both disabled under reduced motion. The column SHALL
be drag-resizable by its right edge, with the width persisted across
restarts within sane bounds.

#### Scenario: Start a new session

- **WHEN** the user selects an agent and creates a new session from the
  Sessions column header
- **THEN** an empty chat view opens for that agent and the session appears in
  the agent's session list

#### Scenario: Switch sessions

- **WHEN** the user switches from one session to another of the same agent
- **THEN** the transcript of the selected session is displayed and the
  selected entry carries the primary-tint active state

#### Scenario: Sessions column reads as its own region

- **WHEN** the chat view is open on desktop
- **THEN** the session layer renders as a distinct column of the content
  island with its own header, separated from the conversation surface by a
  hairline border

#### Scenario: Delete session with confirmation

- **WHEN** the user activates a session entry's delete affordance and
  confirms
- **THEN** the session disappears from the agent's list and its transcript is
  no longer reachable, while the agent's other sessions remain

#### Scenario: Delete cancelled at confirmation

- **WHEN** the user cancels the delete confirmation
- **THEN** the session and its transcript remain unchanged

#### Scenario: Delete refused while a turn is in flight

- **WHEN** the user confirms deleting a session that has a turn in flight
- **THEN** the deletion is refused, the session and its transcript remain,
  and the UI announces the refusal

#### Scenario: Running session shows an indicator

- **WHEN** a session has a turn in flight (including one paused on an
  approval)
- **THEN** its entry shows an inline spinner before a pulsing title until the
  turn reaches a terminal outcome

#### Scenario: Older sessions behind an expander

- **WHEN** an agent has more than four sessions and no search text is entered
- **THEN** the list shows the four most recent sessions and a control naming
  the number of older sessions, which reveals them when activated

#### Scenario: Active older session stays visible

- **WHEN** the active session is not among the agent's four most recent
- **THEN** it is listed alongside the four most recent while the list is
  capped

#### Scenario: Search lifts the cap

- **WHEN** the user types search text matching six sessions
- **THEN** all six matches are listed

### Requirement: Chat right sidebar

The chat view SHALL provide a right sidebar showing the current session's
identity: the agent (with its avatar) and the session's effective model — its
override when set, otherwise the agent's configured model, with its effort
when set. The context-window usage indicator SHALL live on the composer's
rail, not in the sidebar. Collapsing the sidebar MUST NOT interrupt an
in-flight turn, and the docked sidebar SHALL be drag-resizable by its left
edge with the width persisted across restarts.

Activating the context indicator SHALL open a breakdown popover above the
composer showing: the used total against the limit, a stacked bar and legend
of the used context's composition — instructions, tools and skills, files,
conversation, and the remainder attributable to server-side context — the
headroom to the limit, and the last turn's input and output tokens. Rows
whose values are client estimates SHALL be labeled as estimates and MUST NOT
be scaled to match the reported total; the total SHALL always be the
reported used value. The popover SHALL render in both themes, its opening
animation MUST be disabled under reduced motion, and it SHALL NOT open when
no usage has been reported.

#### Scenario: Context indicator

- **WHEN** the user views a session with accumulated turns
- **THEN** the composer's rail shows context usage relative to the model's
  limit

#### Scenario: Sidebar reflects the session override

- **WHEN** the user picks a session-level model different from the agent's
  default
- **THEN** the sidebar shows the session's effective model, and the context
  indicator uses that model's context limit

#### Scenario: Context breakdown popover

- **WHEN** the user activates the context indicator on a session with
  accumulated turns
- **THEN** a popover opens showing the used total against the limit, the
  stacked composition bar with its legend, the headroom, and the last turn's
  input and output tokens

#### Scenario: Estimates are labeled

- **WHEN** the popover shows a composition row derived from a client estimate
- **THEN** the row carries an "estimated" caption and the rows together never
  exceed or get stretched to the reported used total

#### Scenario: No popover before the first turn

- **WHEN** the session has no reported usage yet
- **THEN** no context indicator is shown and no popover can open

### Requirement: Message copy action

Each assistant message SHALL offer a copy action, revealed on hover or focus
alongside its other message actions, that copies the message's source text to
the clipboard and shows a transient confirmation. A copy that the platform
refuses MUST be announced to the user rather than failing silently.

#### Scenario: Copy an assistant message

- **WHEN** the user activates the copy action on an assistant message
- **THEN** the message's source text is placed on the clipboard and the
  action shows a confirmation that fades after a moment

#### Scenario: Copy refused

- **WHEN** the platform refuses clipboard access
- **THEN** the user is told the copy failed

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

### Requirement: Streaming-safe markdown

Partial markdown rendered mid-stream MUST NOT produce transient artifacts
that misrepresent the text: an unclosed code fence MUST NOT swallow the
following text as code, and an unclosed emphasis marker MUST NOT style the
remainder of the message. The completed message SHALL render identically to
the same text rendered without streaming.

#### Scenario: Unclosed fence mid-stream

- **WHEN** a streaming message has opened a code fence that has not yet
  closed
- **THEN** only the fenced text so far renders as code and the message does
  not flash a block that later disappears

#### Scenario: Final render matches a static render

- **WHEN** a streamed message completes
- **THEN** its rendering is identical to rendering the same complete text in
  one pass
