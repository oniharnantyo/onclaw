# chat-panels Specification

## Purpose

Provide side surfaces over the conversation: a tabbed right panel that previews workspace files, prompt documents, and subagent trajectory files, plus a floating run card summarizing plan progress and background tasks.

## Requirements

### Requirement: Right panel shell

The chat view SHALL provide a single right bar that presents one preview at a time as tabs. When no tab is open the bar SHALL show the session's identity view (the agent avatar, name, and effective model); opening a preview SHALL replace that identity in place — the identity and the preview MUST NOT render as separate columns — and closing the last open tab SHALL return the bar to its identity view. While one or more tabs are open, the tab strip SHALL lead with a pinned identity tab — labelled by the agent's avatar, with the agent's name as its accessible label — that MUST NOT carry a close affordance and MUST NOT be removed or reordered; activating it SHALL show the identity view in the same bar while every open tab stays open in the strip, and opening or re-activating a preview from there SHALL activate that preview. Closing a tab that is not the active one while the identity view is shown SHALL keep the identity view showing. The bar docks as a resizable column at wide widths and presents previews as a full-height overlay sheet at narrow widths, remembers its width across sessions, and opens or closes without interrupting an in-flight turn. Each preview tab SHALL carry its own close affordance, and switching chat sessions SHALL reset the panel to the identity view.

#### Scenario: Open panel during a turn

- **WHEN** the user opens a preview while the agent is responding
- **THEN** the preview replaces the identity view in the same bar beside the transcript and the turn continues uninterrupted

#### Scenario: Preview replaces the identity view

- **WHEN** the user opens a file preview while the bar shows the agent identity
- **THEN** the same bar shows the preview in place of the identity, and no second column appears

#### Scenario: Closing the last tab restores identity

- **WHEN** the user closes the only open preview tab
- **THEN** the bar shows the session's identity view again

#### Scenario: Panel state resets on session switch

- **WHEN** the user switches to a different session
- **THEN** the panel is reset to the identity view, ready to be reopened for the new session's content

#### Scenario: Pinned identity tab returns to the identity view with tabs open

- **WHEN** the user activates the pinned identity tab while one or more preview tabs are open
- **THEN** the bar shows the session's identity view and the strip still lists every open preview tab

#### Scenario: Identity tab cannot be closed

- **WHEN** one or more preview tabs are open
- **THEN** the strip's first entry is the identity tab, it offers no close control, and no tab strip shows at all once every preview tab is closed

#### Scenario: Preview opened from the identity tab takes over

- **WHEN** the user opens a preview, or re-activates an already open one, while the identity tab is active
- **THEN** that preview becomes the active tab and replaces the identity view in place

#### Scenario: Closing a background tab keeps the identity showing

- **WHEN** the user closes a preview tab that is not the active one while the identity tab is active
- **THEN** the identity view stays shown and the remaining preview tabs stay listed

#### Scenario: Identity reachable in the narrow overlay

- **WHEN** the view is narrower than the docked breakpoint and the user activates the pinned identity tab in the overlay sheet
- **THEN** the sheet shows the session's identity view

### Requirement: Preview sources

The panel SHALL render previews for: files inside an agent's workspace opened by path (read through a workspace-scoped reader that MUST NOT reach outside the agent's directories), the agent's three prompt documents, knowledge documents opened from the knowledge panel or the Knowledge menu (read through a knowledge-scoped reader that MUST NOT reach outside the knowledge roots), and a delegated task's trajectory file opened by activating its row in the run card's agents lane — read through the same workspace-scoped reader at the task's recorded transcript path. A workspace file whose name ends in `.pdf` SHALL render as a document — the same renderer knowledge PDFs use — with its bytes fetched through a workspace-scoped binary read under the same jail as the text reader; every other workspace file renders as text. Tool cards, transcript links to files or paths, and artifact chips SHALL open the corresponding preview in the panel. Preview reads MUST be restricted to workspace, knowledge, and trajectory locations regardless of what the link contains.

#### Scenario: File path opens preview

- **WHEN** the user selects a file path offered by a completed tool card
- **THEN** the panel opens a tab showing that file's content from the agent's workspace

#### Scenario: Workspace PDF opens as a document

- **WHEN** the user opens a preview for a workspace file whose name ends in `.pdf`, for example from an artifact chip
- **THEN** the panel renders it as a document rather than as text, read through the jailed workspace reader

#### Scenario: Agent row opens trajectory

- **WHEN** the user activates a delegated task's row in the run card's agents lane
- **THEN** the panel opens a tab rendering that task's trajectory

#### Scenario: Workspace reader stays jailed

- **WHEN** a preview request names a path outside any agent workspace (for example a system file), through either the text or the binary read
- **THEN** the read is refused and no file content is returned

#### Scenario: Knowledge document opens preview

- **WHEN** the user activates a knowledge document in the knowledge panel or the Knowledge menu
- **THEN** the panel opens a tab showing that document's content — text rendered as text, PDF rendered as a document

### Requirement: Run card tasks lane

The chat view SHALL show a floating run card over the transcript while the active session has background subagent tasks, listing each task's description and live status (running with a spinner, or its terminal completed/failed/stopped result), derived from the session's task events so the lanes rebuild correctly after a transcript reload. The card MUST render only while at least one lane has content.

The run card SHALL float at the top-right of the transcript's scrolling viewport, clear of the conversation map, and MUST NOT overlap the composer at any viewport height or scroll position. The card SHALL stay in place while the transcript scrolls beneath it.

#### Scenario: Running task surfaces in the run card

- **WHEN** the agent spawns a background task during a turn
- **THEN** the run card shows the task as running with its description until a terminal status arrives

#### Scenario: Lanes rebuild after reload

- **WHEN** the session is reloaded after a task completed earlier
- **THEN** the run card shows the task's terminal result derived from the replayed events

#### Scenario: Run card stays clear of the composer

- **WHEN** the run card is visible and the window is short enough that the transcript viewport is only a few lines tall
- **THEN** the card sits at the top-right of the transcript viewport and no part of it covers the composer or the conversation map

#### Scenario: Run card holds while the transcript scrolls

- **WHEN** the user scrolls the transcript while the run card is visible
- **THEN** the card remains anchored at the viewport's top-right as the entries move beneath it

### Requirement: Run card plan lane

The run card SHALL show the session's current plan as a checklist derived from the latest plan-list tool activity, with each item's pending/active/done/failed state, updating live during the turn. The plan lane SHALL render inside the same floating card as the tasks lane and shares its anchoring. When the session has no plan data, the plan lane MUST not render, and with no lane content the run card is hidden entirely.

#### Scenario: Plan updates live

- **WHEN** the agent rewrites its plan mid-turn
- **THEN** the run card checklist reflects the new items and their states

#### Scenario: No plan, no lane

- **WHEN** the session has not used the plan tool
- **THEN** the run card does not show a plan lane (and hides entirely if the tasks lane is also empty)

### Requirement: Subagent trajectory preview

A trajectory tab SHALL render the delegated task's recorded transcript as the child's conversation: its task prompt as a user-style message, text steps as markdown, reasoning steps as collapsible reasoning disclosures, tool steps as tool-style cards, and the terminal result as the child's final answer. While the delegated task is running, an open trajectory tab SHALL refresh as the transcript file grows and MUST stop refreshing once the task reaches a terminal state. A trajectory path that cannot be read SHALL render the read error outcome, never fabricated content.

#### Scenario: Trajectory renders like the transcript

- **WHEN** the user opens a completed task's trajectory
- **THEN** the tab shows the child's task prompt, its reasoning and tool steps in the transcript's element shapes, and its final answer

#### Scenario: Running trajectory grows live

- **WHEN** a running task's trajectory tab is open and the child records new steps
- **THEN** the tab shows the appended steps without being reopened, and stops refreshing when the task completes

#### Scenario: Unresolvable trajectory shows the error

- **WHEN** a recorded transcript path can no longer be read (for example a session recorded before the trajectory layout changed)
- **THEN** the tab shows the read error outcome and no content

### Requirement: Markdown preview toggle

A workspace file or knowledge text document preview whose name ends in `.md` or `.markdown` SHALL offer a toggle between the raw text and the rendered markdown, with the raw text as the default. The rendered view SHALL use the transcript's markdown pipeline (GFM tables and lists, code blocks) so previews match the transcript's rendering. Previews of files with other extensions SHALL NOT offer the toggle.

#### Scenario: Raw stays default

- **WHEN** the user opens a markdown file's preview
- **THEN** the tab shows the raw text and a preview toggle is available

#### Scenario: Preview renders markdown

- **WHEN** the user activates the preview toggle on a markdown file
- **THEN** the file renders as formatted markdown matching the transcript's rendering

#### Scenario: Toggle only for markdown

- **WHEN** the user opens a preview for a file without a markdown extension
- **THEN** the tab shows only the raw text with no preview toggle

### Requirement: Knowledge panel

The right bar SHALL offer a knowledge panel for the session's agent, opened from the bar's identity view, listing the agent's scoped documents and the global documents with name, description, and a scope marker. The panel SHALL offer upload defaulting to the session agent's scope with global as an explicit choice, a promote action on agent-scoped documents, and activation opens the document's preview as a tab in the bar. Uploads and scope changes from the panel apply to the shared knowledge corpus immediately, and the panel reflects documents the agent saves mid-turn without being reopened.

#### Scenario: Upload defaults to the agent's scope

- **WHEN** the user uploads a document through the knowledge panel without changing the scope choice
- **THEN** the document is saved to the session agent's scope and listed with an agent scope marker

#### Scenario: Mid-turn save appears in the panel

- **WHEN** the agent saves a document during a turn while the knowledge panel is open
- **THEN** the document appears in the panel's list without reopening it

#### Scenario: Promote from the panel

- **WHEN** the user promotes an agent-scoped document from the knowledge panel
- **THEN** the document moves to the global scope and is listed as global

### Requirement: Identity view live avatar

Whenever the identity view is shown — with no tab open, or with the pinned identity tab active while tabs are open — the identity view's agent avatar SHALL render as a live face whose state is a pure derivation from the active session's transcript entries, resolved in precedence order: no turn in flight → idle; a message sent with no event yet → listening; a turn running with no output yet, or with reasoning streaming → thinking; text streaming → speaking; a tool running at the turn's tail → tool call; an unresolved approval → listening with a question badge; an inline error → error, held until the next user message. Terminal turn outcomes SHALL produce one-shot reactions on the face — a completed turn a nod with a sparkle, a cancelled turn a shake with a sweat drop, a failed turn a shake while the error face holds — and these reactions MUST fire only from the live event path, never from a replayed or reloaded transcript, and never from a background session's events while another session is viewed. The face SHALL idle with ambient life (blink, saccades, sway), its gaze MUST follow the pointer across the window while the identity view is shown, and it MUST doze off after idle inactivity, waking when a turn starts or the pointer returns. Under `prefers-reduced-motion: reduce` the identity avatar MUST render as a still picture — no live props and no injected keyframes — consistent with the ui-conventions "Reduced-motion compliance" requirement. After a transcript reload the face MUST rest on the same state the live view ended in — a failed last turn on the error face, a completed one on idle — and nothing MUST move on reload. At most one live avatar instance SHALL exist at a time. Every other avatar surface — agent cards, the agent list, the configuration pane, scheduler rows, the picker preview, and the pinned identity tab's label avatar — MUST render static exactly as before, with no timers and no injected styles.

#### Scenario: Message sent before the first event

- **WHEN** the user sends a message and the agent has not yet emitted an event
- **THEN** the identity avatar shows the listening state

#### Scenario: Reasoning streams as thinking

- **WHEN** the in-flight turn is streaming reasoning with no assistant text output yet
- **THEN** the identity avatar shows the thinking state

#### Scenario: Tool runs at the turn's tail

- **WHEN** a tool is running as the last activity of the in-flight turn
- **THEN** the identity avatar shows the tool-call state

#### Scenario: Text streams as speaking

- **WHEN** the agent's reply is streaming text
- **THEN** the identity avatar shows the speaking state

#### Scenario: Unresolved approval shows a question badge

- **WHEN** the turn stops on an approval the user has not resolved
- **THEN** the identity avatar shows the listening state with a question badge

#### Scenario: Completed turn nods

- **WHEN** a turn completes on the live event path
- **THEN** the avatar nods once with a sparkle as a one-shot reaction

#### Scenario: Cancelled turn shakes

- **WHEN** the user cancels the in-flight turn
- **THEN** the avatar shakes once with a sweat drop as a one-shot reaction

#### Scenario: Failed turn holds the error face

- **WHEN** a turn fails with an inline error
- **THEN** the avatar shakes once, rests on the error face, and keeps it until the user sends the next message

#### Scenario: Reload of a failed turn rests on the error face

- **WHEN** a session whose last turn failed is reloaded
- **THEN** the avatar rests on the error face derived from the replayed entries and nothing animates on reload

#### Scenario: Reload of a completed turn rests idle

- **WHEN** a session whose last turn completed is reloaded
- **THEN** the avatar rests on the idle state and no completion reaction plays

#### Scenario: Background session's outcome does not move the viewed face

- **WHEN** a session other than the viewed one receives its terminal turn event
- **THEN** the viewed session's avatar shows no reaction

#### Scenario: Reduced motion renders a still picture

- **WHEN** the system requests reduced motion
- **THEN** the identity avatar renders as a still picture with no live props and no injected keyframes

#### Scenario: Reaction dropped while the identity view is replaced

- **WHEN** a turn reaches its terminal outcome while a preview tab has replaced the identity view
- **THEN** the reaction is dropped, not deferred, and reopening the identity view after the turn — by closing the last tab or by activating the pinned identity tab — plays no gesture

#### Scenario: Identity tab shows the live face

- **WHEN** the user activates the pinned identity tab while a turn is in flight
- **THEN** the identity view's avatar shows the live state derived for that turn while the strip's label avatar stays still

#### Scenario: Non-identity surfaces stay still

- **WHEN** the user views agent cards, the agent list, the configuration pane, scheduler rows, the picker preview, or the pinned identity tab's label avatar
- **THEN** each avatar renders static exactly as before, with no timers and no injected styles
