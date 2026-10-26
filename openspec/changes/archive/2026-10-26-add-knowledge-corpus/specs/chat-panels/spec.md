# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
