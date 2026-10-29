# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
