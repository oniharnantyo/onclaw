# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, and logs under `~/.buddies/logs/`. The directory structure SHALL be created on first run if missing.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, workspaces directory, and logs directory are created

#### Scenario: Agent workspace directory lives under workspaces

- **WHEN** an agent with slug "scout" is created
- **THEN** its prompt documents live at `~/.buddies/workspaces/scout/`

### Requirement: Configuration persisted in the database

Agent definitions, provider settings, MCP server settings, hooks, and gateway settings SHALL be stored in the SQLite database. An agent's prompt documents are the exception: they SHALL live as files in the agent's workspace directory, with the database row holding the slug, brief, prompt status, and the agent's stored procedural-avatar config (opaque JSON, empty when the avatar is derived from the id) but no document content. Every configuration change MUST be applied atomically and durably: a change is either fully persisted or not at all, and a crash between steps never leaves partial configuration.

#### Scenario: Config survives restart

- **WHEN** the user edits an agent's model and quits the app, then starts it again
- **THEN** the agent shows the edited model

#### Scenario: Document edit survives restart

- **WHEN** the user edits an agent's SOUL document and quits the app, then starts it again
- **THEN** the document file still holds the edited content

## ADDED Requirements

### Requirement: Agent workspace lifecycle

Each agent SHALL have exactly one workspace directory named by its slug. The directory SHALL be created when the agent is created and removed when the agent is deleted; deleting an agent MUST NOT remove its session history, which stays in the database.

#### Scenario: Workspace removed on delete

- **WHEN** the user deletes an agent after generating its documents
- **THEN** the agent's workspace directory is gone while its past sessions remain listed in the data store
