# agent-todos — Delta

## Purpose

Durable agent task planning: a `todo_write` tool with rewrite semantics backed by a real per-session store, a `todo_read` companion, and open-items visibility — so an agent's plan survives compaction, outlives the transcript, and is queryable without event-log archaeology.

## ADDED Requirements

### Requirement: Todo write tool
The backend SHALL provide a built-in `todo_write` tool whose schema accepts a full item list — each item carrying a stable item key, display text, a status of `pending`, `active`, `done`, or `failed`, and an optional failure reason. Each call SHALL atomically replace the calling session's list: rows keyed by existing item keys are updated (restyling, not duplicating), keys absent from the call are deleted, new keys are inserted, and the session's revision counter increments. Invalid lists (unknown status, missing text or key) SHALL be rejected with a tool error the model can correct, without mutating stored state. The tool performs no filesystem, network, or shell effects.

#### Scenario: Rewrite replaces the list atomically
- **WHEN** a session's stored list has items A, B, C and the model calls todo_write with A (done), B (failed, reason), and D
- **THEN** A and B update in place, C is deleted, D is inserted, the revision increments, and no partial application survives a failure

#### Scenario: Invalid list rejected without mutation
- **WHEN** the model calls todo_write with an item whose status is not one of the four allowed values
- **THEN** the tool returns a validation error and the stored list is unchanged

### Requirement: Todo storage
Todos SHALL persist in a dedicated `agent_todos` table scoped by workspace and agent, keyed by session with per-item uniqueness (`session_id`, `item_key`), carrying status, optional reason, revision, and timestamps, with tenant isolation enforced at the data layer (workspace-scoped access) and cascade deletion with their workspace or agent. The current list SHALL be readable as indexed rows — determining the open items MUST NOT require scanning session event payloads. The transcript event log remains the revision history; the table is the current state.

#### Scenario: Open items are a query, not a scan
- **WHEN** a client asks for an agent's open todo items in a workspace
- **THEN** the answer comes from an indexed query over the todo store, not from searching event payloads

#### Scenario: Plan survives compaction and reloads
- **WHEN** a session is compacted and later reopened
- **THEN** the session's current todo list reads back from the todo store unchanged

### Requirement: Todo read access
The backend SHALL provide a built-in `todo_read` tool returning the calling session's current list with statuses and revision. The agent's system context SHALL also carry a compact one-line summary of open items (count by status) so the model can re-ground its plan after compaction without spending a call; the full list remains behind `todo_read`.

#### Scenario: Model re-grounds after compaction
- **WHEN** a session's window has been compacted and the model needs the plan
- **THEN** the context summary shows the open-item counts and a todo_read call returns the full current list

### Requirement: Todo catalog exposure
`todo_write` and `todo_read` SHALL appear in the tool catalog as toggleable entries with display name, description, group, and icon key, exposed per agent like other built-ins. Agents without the tool exposed SHALL produce no todo rendering anywhere (present-only; no empty state).

#### Scenario: Present only when exposed
- **WHEN** an agent configures its exposed tools without Todos
- **THEN** the model cannot call todo_write and the transcript renders no todo elements for that agent's turns
