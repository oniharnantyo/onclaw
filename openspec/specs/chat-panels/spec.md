# chat-panels Specification

## Purpose

Provide side surfaces over the conversation: a tabbed right panel that previews workspace files, prompt documents, and subagent trajectory files, plus a floating run card summarizing plan progress and background tasks.

## Requirements

### Requirement: Right panel shell

The chat view SHALL provide a single right bar that presents one preview at a time as tabs. When no tab is open the bar SHALL show the session's identity view (the agent avatar, name, and effective model); opening a preview SHALL replace that identity in place — the identity and the preview MUST NOT render as separate columns — and closing the last open tab SHALL return the bar to its identity view. The bar docks as a resizable column at wide widths and presents previews as a full-height overlay sheet at narrow widths, remembers its width across sessions, and opens or closes without interrupting an in-flight turn. Each tab SHALL carry its own close affordance, and switching chat sessions SHALL reset the panel to the identity view.

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

### Requirement: Preview sources

The panel SHALL render previews for: files inside an agent's workspace opened by path (read through a workspace-scoped reader that MUST NOT reach outside the agent's directories), the agent's three prompt documents, knowledge documents opened from the knowledge panel or the Knowledge menu (read through a knowledge-scoped reader that MUST NOT reach outside the knowledge roots), and a delegated task's trajectory file opened by activating its row in the run card's agents lane — read through the same workspace-scoped reader at the task's recorded transcript path. Tool cards and transcript links to files or paths SHALL open the corresponding preview in the panel. Preview reads MUST be restricted to workspace, knowledge, and trajectory locations regardless of what the link contains.

#### Scenario: File path opens preview

- **WHEN** the user selects a file path offered by a completed tool card
- **THEN** the panel opens a tab showing that file's content from the agent's workspace

#### Scenario: Agent row opens trajectory

- **WHEN** the user activates a delegated task's row in the run card's agents lane
- **THEN** the panel opens a tab rendering that task's trajectory

#### Scenario: Workspace reader stays jailed

- **WHEN** a preview request names a path outside any agent workspace (for example a system file)
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
