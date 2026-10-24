# Spec Delta

## MODIFIED Requirements

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

The panel SHALL render previews for: files inside an agent's workspace opened by path (read through a workspace-scoped reader that MUST NOT reach outside the agent's directories), the agent's three prompt documents, and a delegated task's trajectory file opened by activating its row in the run card's agents lane — read through the same workspace-scoped reader at the task's recorded transcript path. Tool cards and transcript links to files or paths SHALL open the corresponding preview in the panel. Preview reads MUST be restricted to workspace and trajectory locations regardless of what the link contains.

#### Scenario: File path opens preview

- **WHEN** the user selects a file path offered by a completed tool card
- **THEN** the panel opens a tab showing that file's content from the agent's workspace

#### Scenario: Agent row opens trajectory

- **WHEN** the user activates a delegated task's row in the run card's agents lane
- **THEN** the panel opens a tab rendering that task's trajectory

#### Scenario: Workspace reader stays jailed

- **WHEN** a preview request names a path outside any agent workspace (for example a system file)
- **THEN** the read is refused and no file content is returned

## ADDED Requirements

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

A workspace file preview whose name ends in `.md` or `.markdown` SHALL offer a toggle between the raw text and the rendered markdown, with the raw text as the default. The rendered view SHALL use the transcript's markdown pipeline (GFM tables and lists, code blocks) so previews match the transcript's rendering. Previews of files with other extensions SHALL NOT offer the toggle.

#### Scenario: Raw stays default

- **WHEN** the user opens a markdown file's preview
- **THEN** the tab shows the raw text and a preview toggle is available

#### Scenario: Preview renders markdown

- **WHEN** the user activates the preview toggle on a markdown file
- **THEN** the file renders as formatted markdown matching the transcript's rendering

#### Scenario: Toggle only for markdown

- **WHEN** the user opens a preview for a file without a markdown extension
- **THEN** the tab shows only the raw text with no preview toggle
