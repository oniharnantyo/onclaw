# Spec Delta

## MODIFIED Requirements

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
