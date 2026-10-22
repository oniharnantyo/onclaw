# data-store Specification

## Purpose

Persist all application configuration and session history in a single-user SQLite database under `~/.buddies`, so both hosts and background services read and write consistent, durable state with no external database dependency.

## Requirements

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

### Requirement: Configuration persisted in the database

Agent definitions, provider settings, MCP server settings, hooks, and gateway settings SHALL be stored in the SQLite database. An agent's prompt documents are the exception: they SHALL live as files in the agent's workspace directory, with the database row holding the slug, brief, prompt status, and the agent's stored procedural-avatar config (opaque JSON, empty when the avatar is derived from the id) but no document content. Every configuration change MUST be applied atomically and durably: a change is either fully persisted or not at all, and a crash between steps never leaves partial configuration.

#### Scenario: Config survives restart

- **WHEN** the user edits an agent's model and quits the app, then starts it again
- **THEN** the agent shows the edited model

#### Scenario: Document edit survives restart

- **WHEN** the user edits an agent's SOUL document and quits the app, then starts it again
- **THEN** the document file still holds the edited content

### Requirement: Agent workspace lifecycle

Each agent SHALL have exactly one workspace directory named by its slug. The directory SHALL be created when the agent is created and removed when the agent is deleted; deleting an agent MUST NOT remove its session history, which stays in the database.

#### Scenario: Workspace removed on delete

- **WHEN** the user deletes an agent after generating its documents
- **THEN** the agent's workspace directory is gone while its past sessions remain listed in the data store

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

### Requirement: Provider model rows

The store SHALL persist each provider's model list as model rows: a model id
unique per provider, the model's context window in tokens, its image-input,
PDF-input, and video-input capability flags, an ordered reasoning-effort
list of free-form values (empty when undeclared), and an enabled flag. Row
identity SHALL be store-assigned and immutable; enabling, capability fields,
the effort list, and the model id's row are user-editable configuration.
Deleting a provider MUST remove its model rows. Forward-only migrations
SHALL create the table and seed existing canonical-type providers' rows from
the built-in catalog, and SHALL add the effort list and video-input fields
to existing rows without data loss.

#### Scenario: Model ids are unique per provider

- **WHEN** a model row is created with a model id the provider already has
- **THEN** the store rejects it as a duplicate

#### Scenario: Deleting a provider removes its models

- **WHEN** a provider is deleted
- **THEN** none of its model rows remain

#### Scenario: Migration seeds canonical providers

- **WHEN** the migration runs on a database with existing canonical-type
  providers
- **THEN** each of those providers has the built-in catalog's models as
  enabled rows with their capability data

#### Scenario: Effort list round-trips in order

- **WHEN** a model row is saved with the effort values `none`, `low`, `high`
- **THEN** reading the row returns exactly those values in that order, and
  an undeclared row reads back as empty
