## Purpose

Executes workspace agents: turns a stored agent configuration into streaming, tool-using conversations with durable, replayable history — the runtime that chats, calls tools, manages its context window, and remembers across turns.

## ADDED Requirements

### Requirement: Streaming execution
An agent execution SHALL stream transcript events to its caller as they occur and SHALL end with exactly one terminal event (completed, error, or cancelled). Assistant text and reasoning SHALL be delivered as incremental delta events during generation; deltas SHALL NOT be persisted. Each completed assistant message SHALL be persisted exactly once. An execution interrupted mid-generation SHALL persist an incomplete-message marker so a later reload renders the partial response from durable data alone.

#### Scenario: Deltas stream during generation
- **WHEN** an agent generates a response
- **THEN** the caller receives incremental text/reasoning delta events before the completed message event

#### Scenario: Interrupted generation reloads as partial
- **WHEN** an execution is cancelled or fails mid-stream after partial text was emitted
- **THEN** the persisted history contains an incomplete-message marker and the emitted partial content, and a reload shows the partial response

#### Scenario: Terminal event exactly once
- **WHEN** an execution finishes (any outcome)
- **THEN** the stream ends with exactly one terminal event and no events follow it

### Requirement: Instruction composition
At execution start, the system instruction SHALL be composed in fixed order from: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md`, `USER.md`, `BOOTSTRAP.md`. The first three and the last SHALL be read from the agent's workspace directory. `WORKSPACE.md` SHALL be rendered from the workspace record (name, description). `USER.md` SHALL be rendered from the calling user's record and workspace membership (name, email, role). Composition SHALL happen per execution because `USER.md` varies by caller, and missing documents SHALL be skipped without failing the run.

#### Scenario: Fixed document order
- **WHEN** an execution composes its instruction with all six documents present
- **THEN** the system instruction contains their contents in the order AGENTS, IDENTITY, SOUL, WORKSPACE, USER, BOOTSTRAP

#### Scenario: USER.md varies by caller
- **WHEN** two different members execute the same agent
- **THEN** each execution's instruction carries that member's own name, email, and workspace role

#### Scenario: Missing documents tolerated
- **WHEN** an agent's prompt generation failed and IDENTITY.md/SOUL.md/BOOTSTRAP.md are absent
- **THEN** the execution still runs with the remaining documents (at minimum AGENTS.md plus the two virtual documents)

### Requirement: Tool denylist
The runtime SHALL expose every registered built-in tool to every agent by default. Tools named in the agent's `disabled_tools` SHALL NOT be exposed. Names in `disabled_tools` that match no registered tool SHALL be ignored, not errors.

#### Scenario: Enabled by default
- **WHEN** an agent has an empty `disabled_tools`
- **THEN** every registered built-in tool is available to its executions

#### Scenario: Disabled tool is not exposed
- **WHEN** an agent's `disabled_tools` names a registered tool
- **THEN** its executions cannot invoke that tool and it is absent from the tool surface

### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep) SHALL operate only on paths inside the agent's workspace directory; any resolved path escaping it SHALL be rejected as a tool error, not a crash. The runtime SHALL NOT expose shell/command execution in this scope. The agent's generated prompt documents, its agent-tier skills directory, and the summarization offload file all live inside this directory and are reachable through the file tools.

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

#### Scenario: No shell tool
- **WHEN** an execution lists its available tools
- **THEN** no shell/command-execution tool is present

### Requirement: Three-tier skills
Skills SHALL be discovered from three directories: system (`<ONCLAW_DIR>/skills`), workspace (`<ONCLAW_DIR>/workspaces/<tenant_slug>/skills`), and agent (`<ONCLAW_DIR>/workspaces/<tenant_slug>/agents/<agent_slug>/skills`). The runtime SHALL attach skills from all three tiers except names in the agent's `disabled_skills`; the system tier SHALL be exempt from `disabled_skills` and always attached. On name collision the most specific tier SHALL win: agent > workspace > system. Skills SHALL be consumed through progressive disclosure: metadata lists first, full SKILL.md bodies fetched on demand.

#### Scenario: System skills cannot be disabled
- **WHEN** an agent's `disabled_skills` names a system-tier skill
- **THEN** the skill is still attached to its executions

#### Scenario: Agent authors its own skill
- **WHEN** an agent writes a new SKILL.md document into its own skills directory via a file tool
- **THEN** the skill is discoverable in a subsequent execution of that agent

#### Scenario: Collision precedence
- **WHEN** a skill name exists in both the workspace tier and the agent tier
- **THEN** the agent-tier document is the one served

#### Scenario: System skills synced at startup
- **WHEN** the server starts
- **THEN** the embedded system skill set is mirrored into `<ONCLAW_DIR>/skills` — changed files overwritten, files not in the embedded set removed

### Requirement: Context summarization
When an execution's working-context token count exceeds the resolved context window multiplied by a server-configured safety margin, the runtime SHALL compress the conversation history into a summary generated with the agent's own provider/model, SHALL offload the full pre-compaction history to `transcript.md` inside the agent's workspace directory, SHALL continue the execution with the compressed window plus the agent's recent user messages, and SHALL record the replacement in the session history so the full prior record remains retrievable by replay.

#### Scenario: Trigger fires mid-conversation
- **WHEN** a long thread's token count crosses the resolved context window × margin
- **THEN** the next execution continues from a compressed window and `transcript.md` in the agent directory contains the full prior history

#### Scenario: Compaction is auditable
- **WHEN** a compaction has occurred on a thread
- **THEN** the session history contains a window-replacement record and replaying the full log still yields the pre-compaction messages

### Requirement: Context window resolution
An agent's effective context window SHALL resolve in order: the agent's stored `context_window` when set; otherwise the model catalog's context limit for the agent's provider/model when known; otherwise 200,000 tokens. Resolution is applied when the agent is created or updated: an omitted `context_window` is auto-filled from the catalog (or left unset when the catalog has no limit) and the resolved value is stored, so execution reads a stable stored value (falling back to the 200,000 default when the agent has none).

#### Scenario: Agent override wins
- **WHEN** an agent has `context_window` 50000 and the catalog knows a larger limit for its model
- **THEN** 50000 is used

#### Scenario: Catalog fallback
- **WHEN** an agent has no `context_window` and the catalog knows its model's limit
- **THEN** the catalog limit is used

#### Scenario: Default fallback
- **WHEN** an agent has no `context_window` and the catalog has no limit for its model (e.g. a compatible gateway type)
- **THEN** 200000 is used

### Requirement: Session history
Conversations SHALL be persisted as an append-only, totally ordered event log per thread, independent of any particular model message format. The log SHALL support: cursor pagination (after a given event id, with limit, optionally newest-first), filtering by event kind, and idempotent appends — appending an event whose identity already exists in the thread SHALL NOT duplicate it. A thread's current message window SHALL be reconstructible by replaying the log in order, including across summarization replacements. Execution checkpoints SHALL persist under a resolvable id. Every history query SHALL be workspace-scoped; events from one workspace SHALL be unreachable from another.

#### Scenario: Idempotent append
- **WHEN** the same event (identical thread and event identity) is persisted twice, e.g. after a persist retry
- **THEN** the thread contains exactly one copy — idempotency is guaranteed at the durable store layer, which ignores a conflicting re-insert (Postgres `ON CONFLICT DO NOTHING`); the ADK session adapter additionally rejects a cross-call duplicate EventID with `adk.ErrDuplicateEventID` so a runner retry of an already-committed event is surfaced rather than silently double-applied

#### Scenario: Cursor pagination newest-first
- **WHEN** a client pages a long thread backwards from the end with a limit
- **THEN** each page returns the newest events after the cursor position, newest-first, with the next cursor to continue

#### Scenario: Replay across compaction
- **WHEN** a thread's current window is reconstructed after a summarization replacement
- **THEN** replay yields the replaced (compressed) window as current, and the pre-compaction messages remain present in the log

#### Scenario: Cross-tenant history unreachable
- **WHEN** a member of workspace A addresses a thread belonging to workspace B
- **THEN** the history is not found, indistinguishable from an unknown thread

### Requirement: Cancellation at a safe point
A running execution SHALL be cancellable. Cancellation SHALL take effect at a safe point — the in-flight model call or tool call either completes and is recorded, or is aborted with its partial state marked — and SHALL record a cancel marker in the session history. Cancellation SHALL NOT leave dangling tool-call records that would break the next execution on the thread.

#### Scenario: Cancel between tool calls
- **WHEN** a caller cancels while tool calls are executing
- **THEN** in-flight calls finish or abort cleanly, a cancel marker is recorded, and a subsequent execution on the thread starts from a consistent history
