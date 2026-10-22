# Spec Delta

## MODIFIED Requirements

### Requirement: Sessions organized per agent

The chat menu SHALL list agents and, for each agent, its sessions with latest
activity. The session layer SHALL render as the content island's left column
— separated from the conversation by a hairline border, between the
navigation rail and the conversation — with a header row ("Sessions" plus a
new-session button) and session entries showing the session title and a
relative-time meta line. The active session SHALL be highlighted with the
primary tint. The user SHALL be able to start a new session for an agent and
switch between sessions without losing history. The column SHALL be hidden
while Settings is open.

Each session entry SHALL offer a delete affordance that removes the session
and its transcript only after an explicit confirmation; deleting MUST NOT
affect the agent's other sessions, and the backend MUST refuse the deletion
of a session with a turn in flight. An entry whose session has a turn in
flight SHALL show a running indicator. The column SHALL be drag-resizable by
its right edge, with the width persisted across restarts within sane bounds.

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
- **THEN** its entry shows a running indicator until the turn reaches a
  terminal outcome

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

### Requirement: Chat right sidebar

The chat view SHALL provide a right sidebar showing the current session's
identity: the agent (with its avatar) and the session's effective model — its
override when set, otherwise the agent's configured model, with its effort
when set. The context-window usage indicator SHALL live on the composer's
rail, not in the sidebar. Collapsing the sidebar MUST NOT interrupt an
in-flight turn, and the docked sidebar SHALL be drag-resizable by its left
edge with the width persisted across restarts.

#### Scenario: Context indicator

- **WHEN** the user views a session with accumulated turns
- **THEN** the composer's rail shows context usage relative to the model's
  limit

#### Scenario: Sidebar reflects the session override

- **WHEN** the user picks a session-level model different from the agent's
  default
- **THEN** the sidebar shows the session's effective model, and the context
  indicator uses that model's context limit

## ADDED Requirements

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

### Requirement: Transcript windowing

The transcript SHALL load its most recent window of history first and offer a "load earlier" affordance at the top that prepends older events in chronological order until the session's beginning is reached. Prepending MUST preserve the reader's scroll position, live streaming MUST NOT disturb it, and a page boundary falling inside a turn SHALL present that turn as one continuous entry.

#### Scenario: Load earlier keeps reading position

- **WHEN** the user activates "load earlier" in a scrolled transcript
- **THEN** older entries appear above and the visible message under the cursor stays in place

#### Scenario: Window boundary inside a turn

- **WHEN** an earlier page would end in the middle of a turn's messages
- **THEN** the turn renders as a single unbroken entry after loading

### Requirement: Day dividers

The transcript SHALL insert a day divider between consecutive dated messages of different calendar days, labeled Today, Yesterday, or the date, with the full timestamp available on the divider. No divider SHALL appear before the first dated message.

#### Scenario: Divider between days

- **WHEN** the transcript contains messages from yesterday and today
- **THEN** a labeled divider separates them at the day boundary

#### Scenario: No leading divider

- **WHEN** the transcript is viewed from its first message on a single day
- **THEN** no day divider appears above the first message

### Requirement: Conversation minimap

The chat view SHALL render a minimap beside the transcript on wide widths: one tick per message in the rendered window, with the tick nearest the viewport highlighted during scrolling, and clicking a tick scrolling to its message. The minimap SHALL hide when fewer than two messages are rendered and at narrow widths, and MUST NOT appear in Settings.

#### Scenario: Tick navigates to a message

- **WHEN** the user clicks a tick in the minimap
- **THEN** the transcript scrolls to the corresponding message

#### Scenario: Minimap hides when sparse

- **WHEN** the session has fewer than two rendered messages
- **THEN** no minimap is shown

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