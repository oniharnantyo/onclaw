# Spec Delta

## Purpose

Persist all application configuration and session history in a single-user SQLite database under `~/.buddies`, so both hosts and background services read and write consistent, durable state with no external database dependency.

## ADDED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, skill files under `~/.buddies/skills/`, and logs under `~/.buddies/logs/`. The directory structure SHALL be created on first run if missing.

#### Scenario: First run creates layout

- **WHEN** the app starts with no `~/.buddies` directory
- **THEN** the directory, database, skills directory, and logs directory are created

### Requirement: Configuration persisted in the database

Agent definitions, provider settings, MCP server settings, hooks, and gateway settings SHALL be stored in the SQLite database. Every configuration change MUST be applied atomically and durably: a change is either fully persisted or not at all, and a crash between steps never leaves partial configuration.

#### Scenario: Config survives restart

- **WHEN** the user edits an agent's model and quits the app, then starts it again
- **THEN** the agent shows the edited model

### Requirement: Event-sourced session history

Chat sessions SHALL be persisted as an ordered sequence of events (messages, tool calls, reasoning, errors) with one session per conversation. Reads of a session MUST reconstruct the transcript by replaying its events. Compaction checkpoints SHALL be stored with the session so a compacted context can be rebuilt.

#### Scenario: Transcript survives restart

- **WHEN** a user exchanges messages in a session and restarts the app
- **THEN** the full transcript of that session is shown again

#### Scenario: Session list per agent

- **WHEN** the user opens the chat menu and selects an agent
- **THEN** the sessions previously created for that agent are listed with their latest activity

### Requirement: Forward-only schema migrations

The database schema SHALL be versioned and migrated automatically at startup. Migration MUST run before services accept calls, and a failed migration MUST prevent startup with a clear error rather than running with a stale schema.

#### Scenario: Migration runs before services

- **WHEN** the app starts against a database older than the current schema
- **THEN** pending migrations are applied before the UI can interact with stored data

### Requirement: Concurrent access safety

The database SHALL allow concurrent reads and writes from UI-driven calls and background services without corruption or lost writes, using SQLite's write-ahead logging and bounded wait time for lock contention.

#### Scenario: Write during background activity

- **WHEN** the gateway or an agent turn is writing session events and the user simultaneously saves a settings change
- **THEN** both writes complete successfully
