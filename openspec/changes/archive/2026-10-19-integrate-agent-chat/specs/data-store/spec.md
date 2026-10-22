# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, stored attachment files under `~/.buddies/attachments/`, and logs under `~/.buddies/logs/`. The directory structure SHALL be created on first run if missing.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, workspaces directory, attachments directory, and logs directory are created

#### Scenario: Agent workspace directory lives under workspaces

- **WHEN** an agent with slug "scout" is created
- **THEN** its prompt documents live at `~/.buddies/workspaces/scout/`

#### Scenario: Attachment stored under home layout

- **WHEN** the user attaches a file to a chat message
- **THEN** the stored file lives under `~/.buddies/attachments/` under its capability key

### Requirement: Event-sourced session history

Chat sessions SHALL be persisted as an ordered sequence of events (messages, tool calls, reasoning, approvals, cancellations, errors) with one session per conversation, and each session row SHALL carry its optional model and reasoning-effort overrides — empty values meaning the session inherits its agent's configuration. Reads of a session MUST reconstruct the transcript by replaying its events, including regenerated branch variants. Compaction checkpoints SHALL be stored with the session so a compacted context can be rebuilt, and interrupt/approval checkpoints SHALL be stored alongside them so a paused turn can resume after a restart.

#### Scenario: Transcript survives restart

- **WHEN** a user exchanges messages in a session and restarts the app
- **THEN** the full transcript of that session is shown again

#### Scenario: Session list per agent

- **WHEN** the user opens the chat menu and selects an agent
- **THEN** the sessions previously created for that agent are listed with their latest activity

#### Scenario: Session overrides survive restart

- **WHEN** a user picks a session-level model and effort, restarts the app, and sends a message
- **THEN** the overrides are still stored on the session and apply to that turn

#### Scenario: Regenerated variants replay

- **WHEN** a session with regenerated branch variants is reloaded
- **THEN** replaying the event sequence reconstructs the same variants the picker offered before the restart