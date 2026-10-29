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
first, as compact session entries showing the session title. The active
session SHALL be highlighted with the primary tint. The user SHALL be able
to switch between sessions of any agent without losing history: selecting a
session in another agent's group switches the whole conversation context to
that agent.

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
