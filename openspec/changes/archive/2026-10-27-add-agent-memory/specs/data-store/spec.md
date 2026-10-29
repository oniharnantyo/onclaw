# Spec Delta

## MODIFIED Requirements

### Requirement: Home directory layout

All app data SHALL live under `~/.buddies`: the SQLite database at `~/.buddies/buddies.db`, global skill files under `~/.buddies/skills/`, agent workspace directories under `~/.buddies/workspaces/<agent-slug>/`, stored attachment files under `~/.buddies/attachments/`, knowledge documents under `~/.buddies/knowledge/` (global documents at `~/.buddies/knowledge/global/`, agent-scoped documents at `~/.buddies/knowledge/agents/<agent-slug>/`), and logs under `~/.buddies/logs/`. An agent's workspace SHALL hold its prompt documents at its root, optional per-agent skills under `skills/`, subagent run transcripts under `.tasks/`, user-facing deliverables under `artifacts/` — not dot-prefixed, because the user is meant to find them — and its memory under `memory/` (the `MEMORY.md` index, fact notes, daily notes, and an `archive/` subdirectory), not dot-prefixed either, because the memory is meant to be readable and editable by the user. The directory structure SHALL be created on first run if missing; an agent's `artifacts/` and `memory/` directories SHALL be created on demand the first time a turn runs or a document is generated for that agent, so workspaces created before the directories existed need no migration.

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

#### Scenario: Memory lives under memory

- **WHEN** a turn runs for the agent "scout" whose workspace predates the memory layout
- **THEN** `~/.buddies/workspaces/scout/memory/` and `~/.buddies/workspaces/scout/memory/archive/` exist afterwards and the agent's notes are written there

## ADDED Requirements

### Requirement: Memory state rows

The database SHALL store derived memory state per agent — recall traces (session, turn, origin, query, hit paths with scores, time), per-note retention state (note name, retention score, recall counts, last recall time, first-seen time), staging state for promotion candidates (source reference, signals, first-staged time, promoted flag), and sweep state (watermark, last start, last finish, outcome, counts). These rows SHALL survive restarts, SHALL be rebuildable from the workspace memory files and traces, SHALL hold no note content, and SHALL be removed when their agent is deleted. Writes from a sweep and writes from a turn's `memory_search` SHALL both complete under concurrent access.

#### Scenario: Traces survive restart

- **WHEN** a turn records recall traces and the app restarts
- **THEN** the traces are still present and the next sweep uses them

#### Scenario: Rows hold no note content

- **WHEN** a note's retention state is read
- **THEN** it carries the note name and scores only, and the note's text comes from the workspace file

#### Scenario: Agent deletion removes memory rows

- **WHEN** an agent with recall traces, retention state, staging state, and sweep state is deleted
- **THEN** none of those rows remain while other agents' rows are untouched

#### Scenario: Sweep and turn write concurrently

- **WHEN** a sweep is updating retention state while a turn's `memory_search` records a trace
- **THEN** both writes complete successfully
