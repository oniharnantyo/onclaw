# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, global skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, stored attachment files under `~/.buddies/attachments/`, knowledge documents under `~/.buddies/knowledge/` (global documents at `~/.buddies/knowledge/global/`, agent-scoped documents at `~/.buddies/knowledge/agents/<agent-slug>/`), and logs under `~/.buddies/logs/`. An agent's workspace SHALL hold its prompt documents at its root, optional per-agent skills under `skills/`, subagent run transcripts under `.tasks/`, and user-facing deliverables under `artifacts/` — not dot-prefixed, because the user is meant to find them. The directory structure SHALL be created on first run if missing; an agent's `artifacts/` directory SHALL be created on demand the first time a turn runs or a document is generated for that agent, so workspaces created before the directory existed need no migration.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, workspaces directory, attachments directory, knowledge directory, and logs directory are created

#### Scenario: Agent workspace directory lives under workspaces

- **WHEN** an agent with slug "scout" is created
- **THEN** its prompt documents live at `~/.buddies/workspaces/scout/`

#### Scenario: Subagent transcripts live under .tasks

- **WHEN** a subagent run of the agent "scout" completes
- **THEN** its transcript is stored under `~/.buddies/workspaces/scout/.tasks/`

#### Scenario: Deliverables live under artifacts

- **WHEN** a turn of the agent "scout" produces a file for the user
- **THEN** it lives under `~/.buddies/workspaces/scout/artifacts/`, and the directory exists even when the workspace predates it

#### Scenario: Attachment stored under home layout

- **WHEN** the user attaches a file to a chat message
- **THEN** the stored file lives under `~/.buddies/attachments/` under its capability key

#### Scenario: Knowledge documents live under their scope subtrees

- **WHEN** a global document and an agent-scoped document for slug "scout" exist
- **THEN** their files live at `~/.buddies/knowledge/global/` and `~/.buddies/knowledge/agents/scout/` respectively
