# web-app/chat Specification

## Purpose

The chat experience for agents, channels, and teammate direct messages: transcript rendering with tool-call cards, the composer with slash-command and mention menus, per-agent session lists, and runtime-driven conversation behavior — streaming replies with stop/cancel, edit-and-resubmit, and branch navigation.

## Requirements

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity, and agent tool invocations rendered as expandable cards. A card's collapsed header SHALL show the tool's display name and icon, a human-readable one-line summary of the call (not raw argument JSON), and the latency in milliseconds once completed. A tool card SHALL show a running state while its turn is in flight, an outcome summary when completed, and an error state — the intent form of the summary styled as an error — when the invocation failed. Messages produced by a scheduled run SHALL carry a visible cron-origin marker naming the schedule.

#### Scenario: Tool call card
- **WHEN** an agent message includes a tool invocation that has completed
- **THEN** the collapsed card header shows the tool's display name, a human-readable outcome summary (e.g. "Appended to USER.md" for a memory append; the command verbatim for a shell call), and the latency (e.g. `760ms`) — not truncated raw JSON

#### Scenario: Tool card running state
- **WHEN** an agent message's tool invocation is still executing
- **THEN** the card shows the intent form of the summary (e.g. "Appending to USER.md") with a running indicator instead of a latency value

#### Scenario: Tool card error state
- **WHEN** a tool invocation completes with an error
- **THEN** the card shows the intent form of the summary in an error style with the failure latency, and the expanded view shows the error text

#### Scenario: Summary facts from results
- **WHEN** a completed call's result provides a derivable fact (e.g. a web search returning 8 results)
- **THEN** the outcome summary appends the fact (e.g. "Searched for 'onclaw agent' — 8 results")

#### Scenario: Cron-origin message
- **WHEN** a thread message was produced by schedule "morning-digest"
- **THEN** the message displays a marker naming that schedule

### Requirement: Tool card expanded detail
Expanding a tool card SHALL show labeled fields for the call's arguments — only arguments actually present — with values shaped by kind: monospace chips for paths, selectors, element refs, and URLs; quoted literals for queries and patterns; clamped content blocks with a "show all" affordance for long text. A file-edit call SHALL render the replaced and replacement text as a stacked before/after diff. The expanded view SHALL render the result shaped by its kind (search results as a titled list, text output as a clamped block) and SHALL include the wall-clock time the call started. A raw toggle SHALL expose the full, unmodified argument and result JSON of any card.

#### Scenario: Labeled argument fields
- **WHEN** a memory append card is expanded
- **THEN** the view shows labeled fields for the action and path, and the appended content as a content block — with no fields for absent arguments

#### Scenario: File edit diff
- **WHEN** a file edit card is expanded
- **THEN** the replaced and replacement text render as a stacked before/after diff, clamped like other long content

#### Scenario: Raw JSON toggle
- **WHEN** the raw toggle on any expanded card is activated
- **THEN** the card shows the full unmodified argument and result JSON exactly as delivered by the events

#### Scenario: Unknown-tool fallback
- **WHEN** a tool outside the curated catalog (e.g. an MCP tool) renders a card
- **THEN** the header shows only the tool's display name with running/latency indicators and no fabricated summary sentence, and the expanded view renders parsed JSON as humanized labeled key-value rows — or the raw text when it does not parse

### Requirement: Message send and simulated response
Sending a message SHALL append it to the active session, derive the session title from the first user message (truncated at 42 characters), and produce the agent's reply through the chat runtime bridge. The reply SHALL stream into the transcript incrementally, with a running indicator while the turn is in flight. While a turn is in flight the send control SHALL become a stop control that cancels the turn; partial text SHALL remain in the transcript. Teammate direct messages SHALL NOT trigger an agent turn. In the live UI the reply SHALL always come from the agent runtime — canned simulated replies SHALL NOT be produced (they remain available to test fixtures only); when live chat is unavailable the connect state governs instead.

#### Scenario: Direct agent chat
- **WHEN** the user sends a message to agent "Atlas"
- **THEN** a running indicator appears, then Atlas's real reply streams in incrementally

#### Scenario: Teammate direct message
- **WHEN** the user sends a message in a teammate DM
- **THEN** the message is appended and no agent turn occurs

#### Scenario: Stop control
- **WHEN** the user presses stop while Atlas is responding
- **THEN** the turn cancels, the running indicator clears, and partial text remains

#### Scenario: No canned fallback
- **WHEN** live chat is unavailable and the user sends a message to an agent
- **THEN** no simulated reply is generated; the connect state is shown instead

### Requirement: Slash commands
Typing `/` at the start of the composer SHALL open a filtered command menu (`/tools`, `/model`, `/schedule`, `/reset`, `/help`) navigable by arrow keys with Enter completing the command. `/tools`, `/model`, and `/help` SHALL produce scripted replies derived from the agent's actual configuration. `/reset` SHALL clear the active session without contacting the agent. `/schedule` SHALL reply and then open the schedule editor.

#### Scenario: Scripted reply
- **WHEN** the user sends `/model` to an agent running `claude-sonnet-5` at temperature 0.3
- **THEN** the reply states that model and temperature

#### Scenario: Reset
- **WHEN** the user sends `/reset`
- **THEN** the active session's messages are cleared and a confirmation toast appears

### Requirement: Skill invocation menu
In any composer, typing `$` SHALL open a skill invocation menu following the established `/` and `@` menu mechanics: entries list each available skill's name and description, grouped System / Workspace / This agent, filtered by the token typed after `$`, navigable by arrow keys, with Enter (or click) replacing the token with `$name ` and returning focus to the input. Skills whose tier is unavailable in the current surface (a disabled workspace skill, or another agent's skills) SHALL NOT appear. An unmatched `$name` token sends as ordinary text.

#### Scenario: Menu opens and filters
- **WHEN** the user types `$web` in a direct chat with an agent
- **THEN** the menu shows the `web-research` entry (name and description) and arrow keys move selection

#### Scenario: Pick inserts the token
- **WHEN** the user picks `web-research` from the menu
- **THEN** the composer text contains `$web-research ` with focus back in the input, ready for the rest of the message

#### Scenario: Disabled skill absent from the menu
- **WHEN** a workspace skill's master switch is off
- **THEN** no menu in that workspace lists it

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

### Requirement: Tool approval prompt
When an execution pauses on a dangerous shell command, the transcript SHALL render a pending-approval card in place of the tool-call result: the card SHALL show the command text with an approve and a deny action, and SHALL replace itself with the eventual tool-call result (output or denial notice) once the approval is resolved. The card SHALL render for approvals that are pending from a previous page load or server restart, not only for live interruptions. While the approval is pending, the turn SHALL be presented as paused rather than failed or completed.

#### Scenario: Live approval card
- **WHEN** an `approval_required` event arrives in the live transcript
- **THEN** a pending-approval card appears with the command text and approve/deny actions

#### Scenario: Resolution replaces the card
- **WHEN** the user approves or denies from the card
- **THEN** the card is replaced by the tool result (command output or denial notice) without a full page reload

#### Scenario: Reloaded pending approval
- **WHEN** the transcript is loaded while an approval is pending from an earlier execution
- **THEN** the pending-approval card renders from durable history and remains actionable

### Requirement: Reasoning display
An agent message SHALL render the reasoning its turn produced as a collapsible section positioned above the message text. While reasoning is streaming, the section SHALL be visible with its content growing, so the waiting state shows the agent's thought rather than silence; once the turn completes the section SHALL collapse. A turn that produced no reasoning SHALL render no section. A hydrated message whose completed assistant content carries persisted reasoning SHALL render the same section.

#### Scenario: Streaming reasoning is visible
- **WHEN** an agent turn is producing reasoning before its answer text
- **THEN** the message shows a growing reasoning section instead of an empty loading placeholder

#### Scenario: Completed reasoning collapses
- **WHEN** an agent turn with reasoning completes
- **THEN** the reasoning section collapses and can be re-expanded in place

#### Scenario: No reasoning renders nothing
- **WHEN** an agent turn produced no reasoning content
- **THEN** no reasoning section renders on the message

#### Scenario: Hydrated reasoning
- **WHEN** a transcript is hydrated and a completed assistant message carries persisted reasoning
- **THEN** the message renders the collapsible reasoning section from the persisted content

### Requirement: Turn error display
When an agent turn fails, the transcript SHALL render an error entry in the thread at the point of failure carrying the server-provided error message, styled distinctly from normal messages. The entry SHALL NOT depend on a toast for its existence; a toast may accompany it. Key/connection failures SHALL continue to surface the connect state with its retry path instead of an error entry.

#### Scenario: Failed turn shows an entry
- **WHEN** a live turn ends with a terminal provider error
- **THEN** an error entry carrying the error message appears in the thread after the partial content

#### Scenario: Connect state unchanged
- **WHEN** the workspace chat key is missing or a key exchange fails
- **THEN** the connect state with retry renders as today and no error entry is produced

### Requirement: Context meter
For a 1:1 agent chat, the chat header SHALL render a context meter in the top-right control row — a compact bar plus a monospace percentage indicating how much of the agent's effective context window the conversation currently fills. The meter's value SHALL be the latest turn's final-call input tokens divided by the agent's `effective_context_window`, updated when a terminal response event carries usage and restored from the thread's last turn on reload. Hovering SHALL reveal the exact counts (used and window, e.g. `68k / 200k`). The meter SHALL turn amber at or above `summarization_trigger_tokens`. Channels and direct member messages SHALL NOT render a meter. When no turn has produced usage yet, or a terminal event arrives without a usage block, the meter SHALL be hidden rather than show a zero or a fabricated value.

#### Scenario: Meter fills per turn
- **WHEN** a turn completes on an agent chat whose usage reports 68,000 final-call input tokens and whose agent exposes an effective window of 200,000
- **THEN** the header meter shows 34% and, on hover, `68k / 200k`

#### Scenario: Warn at the summarization trigger
- **WHEN** the meter's value reaches or exceeds the agent's `summarization_trigger_tokens`
- **THEN** the meter renders in the amber warn state

#### Scenario: Meter survives reload
- **WHEN** a user reopens a thread whose last turn carried usage
- **THEN** the meter shows that turn's final-call input against the effective window without waiting for a new turn

#### Scenario: Hidden outside agent chats
- **WHEN** the header renders for a channel or a direct member conversation
- **THEN** no context meter appears

#### Scenario: Hidden without usage data
- **WHEN** a fresh thread has no turns yet, or the latest terminal event carries no usage block
- **THEN** the header renders no meter and no placeholder value

### Requirement: Hook enforcement rendering
The transcript SHALL render hook enforcement where it occurs: a tool call prevented by a hook SHALL render as a tool card marked blocked, showing the hook's reason in place of a result; a prompt prevented by a hook SHALL render as a notice entry carrying the reason in place of an assistant reply. Both renderings SHALL persist across reloads, hydrated from the same history the live stream wrote.

#### Scenario: Blocked tool call in the transcript
- **WHEN** a pre-tool hook blocks the agent's shell call during a live chat
- **THEN** the tool card shows a blocked state with the hook's reason, and the conversation continues from the model's reaction to the block

#### Scenario: Blocked prompt after reload
- **WHEN** a turn whose submitted prompt was blocked by a hook is viewed after a page reload
- **THEN** the transcript shows the notice entry with the reason, and no spinner or empty assistant bubble appears
