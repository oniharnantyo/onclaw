# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, global skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, stored attachment files under `~/.buddies/attachments/`, knowledge documents under `~/.buddies/knowledge/` (global documents at `~/.buddies/knowledge/global/`, agent-scoped documents at `~/.buddies/knowledge/agents/<agent-slug>/`), and logs under `~/.buddies/logs/`. An agent's workspace SHALL hold its prompt documents at its root, optional per-agent skills under `skills/`, and subagent run transcripts under `.tasks/`. The directory structure SHALL be created on first run if missing.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, workspaces directory, attachments directory, knowledge directory, and logs directory are created

#### Scenario: Agent workspace directory lives under workspaces

- **WHEN** an agent with slug "scout" is created
- **THEN** its prompt documents live at `~/.buddies/workspaces/scout/`

#### Scenario: Subagent transcripts live under .tasks

- **WHEN** a subagent run of the agent "scout" completes
- **THEN** its transcript is stored under `~/.buddies/workspaces/scout/.tasks/`

#### Scenario: Attachment stored under home layout

- **WHEN** the user attaches a file to a chat message
- **THEN** the stored file lives under `~/.buddies/attachments/` under its capability key

#### Scenario: Knowledge documents live under their scope subtrees

- **WHEN** a global document and an agent-scoped document for slug "scout" exist
- **THEN** their files live at `~/.buddies/knowledge/global/` and `~/.buddies/knowledge/agents/scout/` respectively

## ADDED Requirements

### Requirement: Document catalog

The database SHALL catalog every knowledge document as a row carrying the filename, MIME type, size, scope (global or agent), the owning agent when scoped, an optional description, the ingestion source (user upload or agent save), and creation time. Rows SHALL survive restarts. A scope change SHALL update the row and move the stored file atomically — a cataloged document always has its file at its scope's location. Rows for agent-scoped documents SHALL be removed when their agent is deleted, together with their files.

#### Scenario: Catalog survives restart

- **WHEN** documents are uploaded and the app restarts
- **THEN** every document is still listed with its scope, description, and source

#### Scenario: Scope change moves row and file together

- **WHEN** an agent-scoped document is promoted to global
- **THEN** the catalog row reflects the global scope and the file exists only under the global subtree

#### Scenario: Agent deletion removes scoped rows

- **WHEN** an agent with scoped knowledge documents is deleted after confirmation
- **THEN** their catalog rows and files are gone while global documents and other agents' documents remain
