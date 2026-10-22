# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, global skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, stored attachment files under `~/.buddies/attachments/`, and logs under `~/.buddies/logs/`. An agent's workspace SHALL hold its prompt documents at its root, optional per-agent skills under `skills/`, and subagent run transcripts under `.tasks/`. The directory structure SHALL be created on first run if missing.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, workspaces directory, attachments directory, and logs directory are created

#### Scenario: Agent workspace directory lives under workspaces

- **WHEN** an agent with slug "scout" is created
- **THEN** its prompt documents live at `~/.buddies/workspaces/scout/`

#### Scenario: Subagent transcripts live under .tasks

- **WHEN** a subagent run of the agent "scout" completes
- **THEN** its transcript is stored under `~/.buddies/workspaces/scout/.tasks/`

#### Scenario: Attachment stored under home layout

- **WHEN** the user attaches a file to a chat message
- **THEN** the stored file lives under `~/.buddies/attachments/` under its capability key

## ADDED Requirements

### Requirement: Per-agent hook assignments

The database SHALL store per-agent hook assignments as ordered rows scoped to the agent, separate from global hooks. Assignments SHALL survive restarts and SHALL be removed when the agent or the assigned hook is deleted.

#### Scenario: Assignment survives restart

- **WHEN** the user assigns a hook to an agent and restarts the app
- **THEN** the assignment is still present and applies to the agent's turns

#### Scenario: Deleting the hook removes assignments

- **WHEN** a hook assigned to agents is deleted
- **THEN** its assignments disappear from every agent

### Requirement: Per-tool settings

The database SHALL store per-tool configuration as key–JSON rows, used for the web_search provider chain (provider order and credentials). Tool settings SHALL survive restarts and MUST be applied atomically.

#### Scenario: Search chain survives restart

- **WHEN** the user saves a web_search provider chain and restarts the app
- **THEN** the chain is still configured and used by web_search
