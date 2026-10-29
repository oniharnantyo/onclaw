# chat-view Specification

## Purpose

Provide the chat menu: per-agent session lists plus the chat view with its right sidebar, porting onclaw-v2's chat affordances (context ring, composer menus, tool-call cards, reasoning bubbles, compaction markers) so single-user chat matches the predecessor's capabilities.

## Requirements

### Requirement: Sessions organized per agent

The chat menu SHALL list every agent as a collapsible group in the session
layer, which renders as the content island's left column — separated from
the conversation by a hairline border, between the navigation rail and the
conversation — with a "Sessions" header row. The column SHALL be hidden
below `md` widths and while Settings is open.

Each group row SHALL show the agent's avatar, the agent's name, the number
of the agent's sessions, and a new-session control; a group any of whose
sessions has a turn in flight SHALL also show a group running dot. Groups
SHALL be collapsed by default, and exactly one group SHALL be expanded at a
time: the active agent's. Any action that moves the conversation context
into an agent — selecting one of its sessions, creating a session for it,
or the session restored at startup — SHALL expand that agent's group and
collapse the previously expanded one.

An expanded group SHALL list that agent's sessions, most recent activity
first, as compact session entries showing the session title and the entry's
last activity as a compact relative time — minutes, hours, days, then a
short calendar date — right-aligned beside the title without increasing the
row's density. The active session SHALL be highlighted with the primary
tint. The user SHALL be able to switch between sessions of any agent
without losing history: selecting a session in another agent's group
switches the whole conversation context to that agent.

Each group row's new-session control SHALL create a session for that agent
and immediately open it — from collapsed and expanded rows alike — and the
control MUST NOT require the agent to be active first.

The unfiltered expanded list SHALL show the agent's four most recent
sessions — plus the active session when it is older than those — behind a
progressive "Load more sessions" control: each activation SHALL append the
next batch of that agent's older sessions, and the control SHALL retire
when the agent's sessions are exhausted. A non-empty search SHALL list
every match regardless of the load-more state.

Each session entry SHALL offer a delete affordance that removes the session
and its transcript only after an explicit confirmation; deleting MUST NOT
affect the agent's other sessions, and the backend MUST refuse the deletion
of a session with a turn in flight. An entry whose session has a turn in
flight SHALL show a running indicator: an inline spinner before the title
with the title pulsing, both disabled under reduced motion. The column
SHALL be drag-resizable by its right edge, with the width persisted across
restarts within sane bounds.

#### Scenario: Start a new session

- **WHEN** the user activates the new-session control on an agent's group
  row while a different agent's session is open
- **THEN** an empty chat view opens for that agent's new session, the
  context switches to that agent, its group expands, and the session
  appears in its list

#### Scenario: Switch sessions

- **WHEN** the user switches from one session to another of the same agent
  within its expanded group
- **THEN** the transcript of the selected session is displayed and the
  selected entry carries the primary-tint active state

#### Scenario: Switch to another agent's session

- **WHEN** the user expands another agent's group and selects one of its
  sessions
- **THEN** the whole conversation context switches to that agent, the
  selected session's transcript is displayed with the active tint, and the
  previously expanded group collapses

#### Scenario: Groups start collapsed

- **WHEN** the app opens on the chat view
- **THEN** every agent's group is collapsed — showing avatar, name, session
  count, and new-session control — except the active agent's, which is
  expanded

#### Scenario: Expanding a group changes nothing else

- **WHEN** the user activates a collapsed group row
- **THEN** that group expands (the previously expanded one collapses), and
  the open conversation and context are unchanged

#### Scenario: Group running dot

- **WHEN** any session of a collapsed agent has a turn in flight
- **THEN** the group row shows the running dot, and expanding the group
  reveals the per-session indicator on the affected entry

#### Scenario: Sessions column reads as its own region

- **WHEN** the chat view is open on desktop
- **THEN** the session layer renders as a distinct column of the content
  island with its own header, separated from the conversation surface by a
  hairline border

#### Scenario: Session entries carry their last activity

- **WHEN** an expanded group lists sessions whose last activities are 3
  minutes, 4 hours, and 9 days old
- **THEN** the entries read "3m", "4h", and a short calendar date beside
  their titles, and the rows keep their compact single-line height

#### Scenario: Delete session with confirmation

- **WHEN** the user activates a session entry's delete affordance and
  confirms
- **THEN** the session disappears from its group's list and its transcript
  is no longer reachable, while the agent's other sessions remain

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
- **THEN** its entry shows an inline spinner before a pulsing title until
  the turn reaches a terminal outcome

#### Scenario: Older sessions behind an expander

- **WHEN** an agent has more than four sessions and no search text is
  entered
- **THEN** the expanded group shows the four most recent sessions and a
  "Load more sessions" control; each activation appends the next batch of
  that agent's older sessions, and the control retires once every session
  of the agent is listed

#### Scenario: Active older session stays visible

- **WHEN** the active session is not among its agent's four most recent
- **THEN** it is listed alongside the four most recent while the list is
  capped

#### Scenario: Search lifts the cap

- **WHEN** the user types search text matching six sessions of one agent
- **THEN** all six matches are listed in that group regardless of how many
  load-more batches had been appended

### Requirement: Session search filter

The Sessions island SHALL provide a search input that filters the visible
session entries by title as the user types. The search SHALL span every
agent's sessions: each group with at least one match SHALL expand to list
its matches, and groups without matches SHALL render as collapsed rows.
Clearing the input SHALL restore the collapse grammar — the active agent's
group expanded, the rest collapsed. Filtering SHALL NOT change which
session is active or reload history.

#### Scenario: Filter sessions by title

- **WHEN** the user types search text matching sessions of two agents
- **THEN** both agents' groups expand and list only their matching
  sessions, while other agents' groups remain collapsed rows

#### Scenario: Clearing the filter

- **WHEN** the user clears the search input
- **THEN** the full grouped list is restored — the active agent's group
  expanded, the rest collapsed — with the previously active session still
  selected

### Requirement: Chat header

The chat view SHALL present a header above the transcript that is a quiet
title bar: it shows the conversation's title — the active session's title,
falling back to the agent's name before any session exists — and nothing
else. The header SHALL carry no avatar, status, or configuration
affordances: turn lifecycle surfaces on the sessions rail's running
indicators and in the transcript, and the session's model and effort are
visible in the composer's pickers. The header's right SHALL host the right
sidebar's toggle, the minimized sidebar's only affordance. The header SHALL
render in both themes and MUST NOT intercept clicks outside its actual
controls.

#### Scenario: Title follows the session selection

- **WHEN** the user switches to another session of the agent
- **THEN** the header shows the newly selected session's title

#### Scenario: Untitled session names itself

- **WHEN** the active session has no title set
- **THEN** the header shows "Untitled session"

#### Scenario: Agent name before a session exists

- **WHEN** the chat is open on an agent with no active session
- **THEN** the header shows the agent's name

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

### Requirement: Regenerate and branch variants

From any assistant message the user SHALL be able to regenerate the answer: a new alternative is produced for that message and the message gains a `n / total` branch picker to move between alternatives, with the newest alternative shown by default. Alternatives SHALL persist with the session so a reloaded transcript still offers the same picker with the same content. Regeneration MUST reach the backend as a real turn — it consumes model capacity and streams live — and changing variants locally SHALL NOT trigger any request. A turn's answer that fails while regenerating SHALL leave the previous alternative selected.

#### Scenario: Regenerate produces an alternative

- **WHEN** the user regenerates an assistant message
- **THEN** the message shows a second variant selected after the new turn streams, with the picker reading 2 of 2, and the transcript retains the original text behind variant 1

#### Scenario: Variants survive reload

- **WHEN** the user reloads the session after regenerating
- **THEN** the message still offers both variants with a working picker

#### Scenario: Switching variants is offline

- **WHEN** the user moves between variants with the picker
- **THEN** the displayed content switches immediately with no loading or network activity

#### Scenario: Failed regeneration keeps the old answer

- **WHEN** a regeneration fails mid-stream
- **THEN** the picker returns to the previous variant, which remains intact

### Requirement: Chat right sidebar

The chat view SHALL provide a single right bar showing the current session's
identity — the agent (with its avatar) and the session's effective model: its
override when set, otherwise the agent's configured model, with its effort
when set — whenever the bar holds no preview. Opening a preview SHALL replace
the identity view in place: the identity and the preview MUST NOT render as
separate columns, and closing the last preview SHALL restore the identity. The
context-window usage indicator SHALL live on the composer's rail, not in the
bar. Collapsing the bar MUST NOT interrupt an in-flight turn, and the docked
bar SHALL be drag-resizable by its left edge with one width persisted across
restarts.

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
- **THEN** the bar shows the session's effective model, and the context
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

### Requirement: Composer affordances

The message composer SHALL support sending with Enter, newline insertion, slash commands, a skill picker, and an @-mention menu, matching onclaw-v2's composer behavior. Queued messages sent while the agent is busy MUST be delivered in order.

The composer SHALL also present three session controls: an agent picker that
switches the chat to the chosen agent's conversation context — that agent's
session list — without changing the stored agent binding of any existing
session; a model picker listing the models available from the session's
provider; and an effort picker offering only the reasoning-effort options the
selected model supports, hidden when the model supports none. An unset model
or effort follows the agent's configured defaults. Once chosen, a choice
SHALL persist on the session, survive reloads, and become the starting
default for that agent's newly created sessions.

While a turn is in flight the send control SHALL become a stop control;
activating it cancels the turn, the partial transcript output remains, and
the composer returns to its send state. If cancellation leaves queued
messages, the next queued message SHALL dispatch without further user
action.

#### Scenario: Slash command menu

- **WHEN** the user types "/" in the composer
- **THEN** a menu of available slash commands opens and filters as the user types

#### Scenario: Message queued while busy

- **WHEN** the user sends a message while the agent is still responding
- **THEN** the message is queued and delivered after the current turn completes, in send order

#### Scenario: Model choice persists on the session

- **WHEN** the user picks a different model in the composer and the page is reloaded
- **THEN** the session still uses the chosen model for its next turn without
  changing the agent's configured default model

#### Scenario: Effort offers only supported options

- **WHEN** the user opens the effort picker for the selected model
- **THEN** only that model's supported reasoning-effort options are listed,
  and no picker is shown when the model supports none

#### Scenario: Agent picker switches conversation context

- **WHEN** the user chooses another agent in the composer's agent picker
- **THEN** the chat switches to that agent and its sessions, and the session
  previously being viewed remains unchanged and bound to its own agent

#### Scenario: Stop control cancels the turn

- **WHEN** the user activates the stop control during streaming
- **THEN** streaming ends, the partial text stays in the transcript, a
  cancelled outcome is recorded for the turn, and the control returns to
  send; with a queued message waiting, it dispatches next

### Requirement: Theme parity

The chat menu SHALL render correctly in both the light and dark app themes, including transcripts, sidebar, composer menus, and tool cards.

#### Scenario: Theme switch

- **WHEN** the user toggles between light and dark themes while viewing a chat
- **THEN** every chat view element renders legibly in the selected theme

### Requirement: Background task progress

The chat view SHALL render background task activity as it happens: a task card while a task is running, its completion or cancellation, and access to the task's output once finished.

#### Scenario: Task lifecycle is visible

- **WHEN** a background task starts, runs, and finishes during a session
- **THEN** the transcript shows the running card and then the completion, and the finished task's output can be viewed

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

### Requirement: Turn attribution

Each assistant turn in the transcript SHALL open with the session agent's
identity: the agent's avatar rendered small and static beside the agent's
name, so the conversation always shows who is speaking. Attribution MUST
NOT mount a second live animated avatar instance: the right bar's identity
face SHALL remain the transcript's only live avatar, and the attribution
stub renders statically under every motion setting. Compaction markers and
user messages MUST NOT carry attribution. Subagent work inside a turn
remains attributed to the session agent: a subagent's identity lives in its
tool card, never in the turn attribution.

#### Scenario: Assistant turn names its agent

- **WHEN** the transcript renders an assistant turn
- **THEN** the turn opens with the session agent's avatar and name above
  its content

#### Scenario: Attribution stays static

- **WHEN** a turn renders while the agent is working, and again under
  reduced motion
- **THEN** the attribution avatar is static in both cases, and the live
  animated face appears only in the right bar

#### Scenario: Subagent work stays attributed to the session agent

- **WHEN** a turn includes a subagent task tool card
- **THEN** the turn's attribution still names the session agent, and the
  subagent is identified inside its tool card

### Requirement: Error presentation

When a turn fails, the transcript SHALL render the failure as a
human-readable card instead of the raw error text: a plain-language
headline naming what failed and, where the failure class is recognized, its
recovery path; the full raw error preserved behind a collapsed
technical-details disclosure; and a Retry action that regenerates the turn
under the existing branch-variant rules. The raw error string MUST NOT be
the card's primary content, and the card MUST announce itself with alert
semantics.

#### Scenario: Provider unreachable

- **WHEN** a turn fails because the model endpoint refused the connection
- **THEN** the card's headline says the agent couldn't reach the model
  provider in plain language, with no internal error codes in the headline

#### Scenario: Raw error stays available

- **WHEN** the error card renders
- **THEN** the complete raw error text is available behind the collapsed
  technical-details disclosure

#### Scenario: Retry regenerates as a branch

- **WHEN** the user activates Retry on a failed turn
- **THEN** a real regeneration starts as a new variant, and the failed
  attempt remains selectable behind the message's branch picker

#### Scenario: Unrecognized failure stays honest

- **WHEN** the failure class is not recognized
- **THEN** the headline is a generic plain-language statement that the turn
  failed, with the raw error still available in the details disclosure

### Requirement: Composer rail economy

The composer rail SHALL keep its resting chrome at the width of the
conversation it serves. The model trigger's label SHALL show the model's
family name — the final `/`-delimited segment of the model id — while the
full id stays in the model picker's menu and on the trigger's tooltip. The
`/` `@` `$` hint row SHALL render only while the composer draft is empty —
no text and no attachments or chips — and SHALL return the moment the draft
empties again. Hiding the hint row MUST NOT disable the triggers behind it:
their popovers open as before. The right bar's model line SHALL name the
provider and the same family label of the session's effective model,
truncating rather than wrapping at ordinary conversation widths, with the
full id exposed on hover. The "default" override and inherit markers on the
trigger and the model line SHALL remain.

#### Scenario: Trigger shows the family name

- **WHEN** the session's model is `openrouter/deepseek/deepseek-chat`
- **THEN** the model trigger reads `deepseek-chat` with the full id on its
  tooltip, and the picker menu still lists each model's full id

#### Scenario: Hint row yields to a typed draft

- **WHEN** the user types into an empty composer so the draft has text
- **THEN** the `/` `@` `$` hint row is not rendered while the text stands

#### Scenario: Hint row yields to attachments alone

- **WHEN** the draft carries an attachment or chip but no text
- **THEN** the hint row stays hidden until the draft empties again

#### Scenario: Hint row returns when the draft empties

- **WHEN** the user removes all text, attachments, and chips from the draft
- **THEN** the hint row renders again and the trigger popovers open
  unchanged

#### Scenario: Right bar's model line truncates

- **WHEN** the right bar shows the session's effective model at an ordinary
  conversation width that does not fit the whole label
- **THEN** the provider name and family label truncate with an ellipsis
  rather than wrapping mid-token, and hovering the line shows the full
  model id
