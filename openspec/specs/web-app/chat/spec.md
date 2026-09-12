# web-app/chat Specification

## Purpose

The chat experience for agents, channels, and teammate direct messages: transcript rendering with tool-call cards, the composer with slash-command and mention menus, per-agent session lists, and runtime-driven conversation behavior — streaming replies with stop/cancel, edit-and-resubmit, and branch navigation.

## Requirements

### Requirement: Transcript rendering
The transcript SHALL render user messages, agent messages, and teammate messages distinctly, with agent messages showing the agent's identity, and agent tool invocations rendered as expandable cards. A card's collapsed header SHALL show the tool's display name and icon, a human-readable one-line summary of the call (not raw argument JSON), and the latency in milliseconds once completed. A tool card SHALL show a running state while its turn is in flight, an outcome summary when completed, and an error state — the intent form of the summary styled as an error — when the invocation failed. Messages produced by a scheduler run SHALL carry a visible scheduler-origin marker naming the schedule.

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
- **THEN** the message displays a scheduler-origin marker naming that schedule### Requirement: Tool card expanded detail
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

### Requirement: Compact command menu
Typing `/` at the start of an agent-chat composer SHALL open a command menu listing only `/compact` (with its description), navigable by arrow keys with Enter completing the command. Submitting `/compact` — with optional trailing text as the focus instruction — SHALL be intercepted by the client and submitted as a compact-command turn; the literal command text SHALL NOT reach the model as a user message and SHALL NOT be persisted as one. In channel and team composers the command menu SHALL NOT open and `/compact` SHALL send as ordinary text. Any other `/foo` input SHALL send as ordinary text in every surface.

#### Scenario: Menu lists only the compact command
- **WHEN** the user types `/` in an agent-chat composer
- **THEN** the menu shows only `/compact` — none of the former `/tools`, `/model`, `/schedule`, `/reset`, `/help` entries appear

#### Scenario: Command is intercepted
- **WHEN** the user submits `/compact keep the API design decisions`
- **THEN** a compact-command turn is submitted with that focus text, no user message containing the command appears in the thread, and the model receives no literal `/compact` text

#### Scenario: Unknown slash input passes through
- **WHEN** the user submits `/frobnicate` in an agent chat
- **THEN** it sends as an ordinary user message

#### Scenario: Channels do not offer commands
- **WHEN** the user types `/compact` in a channel composer
- **THEN** no command menu opens and the text sends as an ordinary message

### Requirement: Compaction status row
While a compact-command turn is running in an agent chat, the thread SHALL show a pending status row ("Compacting context…") in place of any agent reply, and no user message for the command SHALL appear. The status row SHALL be retracted when the turn reaches a terminal state — replaced by the compaction divider on success, and removed without a divider when the turn fails or completes without compacting (error surfacing follows the turn-failure rules).

#### Scenario: Status row during compaction
- **WHEN** the user submits `/compact` and the summarizer is still running
- **THEN** the thread shows the compacting status row with no user pill for the command and no optimistic agent reply

#### Scenario: Status row retracted on quiet completion
- **WHEN** a compact turn completes without producing a compaction (nothing to compact) or fails
- **THEN** the status row is removed and no divider is left behind

### Requirement: Compaction divider
A completed context compaction — manual (`/compact`) or automatic (threshold-triggered) — SHALL render in the transcript as a divider between the older messages and the compacted window, showing `Context compacted` with the working-context token estimates before → after when available. The divider SHALL render identically for live turns and for hydrated history, so past compactions remain visible after reload. The UI SHALL NOT fabricate an agent acknowledgement message for a compaction.

#### Scenario: Manual compaction divider
- **WHEN** a `/compact` turn completes with the context-compacted event carrying token estimates
- **THEN** the transcript shows a `Context compacted · N → M tokens` divider between the prior messages and the compacted window, with no user pill and no agent ack message

#### Scenario: Automatic compaction is visible
- **WHEN** an automatic threshold compaction occurs mid-conversation
- **THEN** the same divider renders at the compaction point

#### Scenario: Divider survives reload
- **WHEN** the user reloads a chat whose session history contains a compaction
- **THEN** the hydrated transcript renders the same divider at the recorded position

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
Each agent chat SHALL support multiple named sessions. The sidebar SHALL list sessions for the active agent chat with controls to start a new session, switch sessions, and delete one. Deleting the last session SHALL spawn a fresh empty session, so a chat never has zero sessions. The session list SHALL be sourced from the server's per-agent session index — scoped to the signed-in user, ordered by last activity (newest first) so continuing an old session moves it to the top — with the browser-local cache serving only as the immediate paint before the server responds and as history for sessions that predate the index. When the user sends a message in a new session, the sidebar SHALL title it immediately from the typed text using the same rule the server applies (first line, trimmed, truncated with an ellipsis), so no blank or stale title is ever shown while the server row is being created. The list SHALL refresh without a manual reload when the user's own turn completes, when the window regains focus or visibility, and when another tab of the same app persists chat state (storage event); a session whose stored title is empty SHALL display the "New chat" fallback. Deleting a session SHALL soft-delete it server-side and remove it locally; if the deleted session was active, an adjacent session SHALL become active, falling back to a fresh empty session when none remains.

#### Scenario: Session lifecycle
- **WHEN** the user deletes the only session of an agent chat
- **THEN** a new empty session titled "New chat" becomes active and the server row is soft-deleted

#### Scenario: List survives a fresh browser
- **WHEN** the user opens the workspace in a browser with no local cache (e.g. incognito)
- **THEN** the sidebar lists the user's server-indexed sessions for the agent, newest activity first, and selecting one hydrates its transcript from the session events endpoint

#### Scenario: Rechat bumps an old session to the top
- **WHEN** the user sends a message in a session that was not the most recently active
- **THEN** that session moves to the top of the sidebar list after the turn's activity is applied

#### Scenario: Optimistic title matches the server
- **WHEN** the user sends the first message of a new session
- **THEN** the sidebar shows the input-derived title immediately and the server-confirmed title from the refreshed list agrees with it

#### Scenario: Refresh without reload
- **WHEN** the user's own turn finishes, the tab regains focus, or another tab writes chat state
- **THEN** the sidebar refetches the session list and applies new, retitled, reordered, or newly running entries without a page reload

### Requirement: Session running indicator
A sidebar session row whose session has a live run SHALL show a leading spinner before the title AND pulse the title (opacity animation) while the run executes; idle rows SHALL carry no running marker at all. The indicator SHALL merge two sources — the app's own run state for the session (instant, no round-trip) and the server list's running flag (runs live in other tabs, devices, or origins) — so exactly one row animates per running session. The pulse SHALL stop when the turn reaches a terminal state locally or when a refreshed list no longer reports the session as running.

#### Scenario: Own run pulses instantly
- **WHEN** the user sends a message in a session
- **THEN** that session's title starts pulsing and its leading spinner starts immediately, before any server confirmation

#### Scenario: Foreign run pulses on refresh
- **WHEN** a run is live on one of the user's sessions from another tab or device and the list refreshes
- **THEN** that session's title pulses and shows the leading spinner even though this tab never started it

#### Scenario: Pulse ends with the run
- **WHEN** the run finishes (or is cancelled) and the state source updates
- **THEN** the title stops pulsing and the spinner disappears without a page reload

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

### Requirement: Attachment composer tray
The agent-chat composer SHALL provide an attachment tray between the message text area and the input controls, fed by three entry paths: the attach button's file picker, clipboard paste of file items into the text area, and drag-and-drop of files onto the chat surface. Each attached file renders as a chip showing an image thumbnail or a document icon, the filename, and the size. Chips SHALL progress through states: uploading (with progress indicator and cancel), ready (with remove), rejected (with the server's reason inline, e.g. size cap exceeded or "export as PDF" for office formats), and failed (with retry — re-uploading the retained file — and remove). Pasted clipboard files without names SHALL be named with a timestamped default. A drag-in-progress SHALL show a dashed drop-target overlay over the message list; drops SHALL be prevented from navigating the browser. Folders and over-cap selections SHALL be rejected with explicit toasts. The tray is per-conversation local state: switching conversations clears it and aborts in-flight uploads.

#### Scenario: Chip lifecycle on a slow upload
- **WHEN** a user picks a 4.8 MB PDF and it uploads over a slow connection
- **THEN** a chip appears immediately in the uploading state with a progress indicator and a cancel control, and becomes a ready chip when the upload completes

#### Scenario: Failed upload offers retry
- **WHEN** an upload fails with a network error mid-flight
- **THEN** the chip shows a failed state with Retry and Remove, and Retry re-uploads the same file without re-picking

#### Scenario: Rejected at the door
- **WHEN** a user attaches `report.docx`
- **THEN** a rejected chip appears naming the reason and the "export as PDF" guidance, and the composer's send state treats it as absent

#### Scenario: Paste a screenshot
- **WHEN** a user presses paste with an image on the clipboard while the composer is focused
- **THEN** an uploading chip appears with a timestamped default name, and the pasted text (if any) is unaffected

#### Scenario: Drag files onto the chat
- **WHEN** a user drags two PNG files over the chat surface
- **THEN** a dashed overlay appears reading "Drop to attach", and dropping adds both files as uploading chips (subject to the per-message cap); dragging text does not show the overlay

#### Scenario: Session switch clears the tray
- **WHEN** the user switches conversations while an upload is in flight
- **THEN** the tray empties and the in-flight upload is aborted

### Requirement: Attachment send gate
Sending SHALL be blocked while any tray chip is uploading. The send control SHALL be enabled when the text is non-empty or at least one chip is ready — attachment-only messages are valid. Attachments MUST never be silently dropped on send: a message that sends carries exactly the ready chips. Regeneration re-sends chip references (ids), not re-uploads.

#### Scenario: Enter mid-upload does not send
- **WHEN** a user presses Enter while a chip is still uploading
- **THEN** nothing sends; the send control is visibly disabled until the chip resolves

#### Scenario: Attachment-only send
- **WHEN** the tray holds a ready image chip and the text area is empty
- **THEN** send is enabled and sends a message whose visible content is the attachment alone

#### Scenario: Optimistic bubble matches hydration
- **WHEN** a message with attachments is sent
- **THEN** the optimistic user bubble renders the same chips (thumbnail/icon, name, size) that a page reload renders from hydrated history

### Requirement: Transcript attachment rendering
User messages in the transcript — live and hydrated alike — SHALL render attachments below the message text: images as inline thumbnails loaded from their capability URLs, documents as an icon chip with filename, media type, size, and a download link. Rejected states never render in the transcript (rejection lives only in the composer). Drop-lane attachments render identically to documents; the pointer note is not user-visible text.

#### Scenario: Image message rendering
- **WHEN** a user message carries one PNG attachment
- **THEN** the bubble shows the message text followed by an inline thumbnail of the image

#### Scenario: Document chip rendering
- **WHEN** a user message carries a PDF attachment
- **THEN** the bubble shows a document chip with the filename, "PDF · 4.8 MB" sizing, and a download affordance

#### Scenario: Reload renders the same chips
- **WHEN** a session with an attachment message is reloaded from history
- **THEN** the user bubble renders the same chips as it did live, sourced from the hydrated attachment metadata

### Requirement: Capability-aware attachment hint
When the user attaches an image or PDF file to a chat whose agent's model does not accept that input kind, the attachment chip SHALL display a soft warning naming the degraded behavior (e.g. "this model can't see images — will attach as reference only"). The warning SHALL NOT block sending. The hint SHALL appear only when capability is affirmatively unsupported; a model with unknown capability shows no hint. The agent's input-modality capability SHALL reach the chat client through the agent data the composer already loads.

#### Scenario: Image chip warns on a text-only model
- **WHEN** an image chip lands in the composer of a chat with an agent whose model is affirmatively text-only
- **THEN** the chip shows the degraded-behavior warning and the send button remains enabled

#### Scenario: No warning for capable or unknown models
- **WHEN** an image chip lands in a chat whose agent's model supports image input, or whose capability is unknown
- **THEN** the chip renders without the warning
