# agent-runtime Specification

## Purpose

Executes workspace agents: turns a stored agent configuration into streaming, tool-using conversations with durable, replayable history — the runtime that chats, calls tools, manages its context window, and remembers across turns.

## Requirements

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
At execution start, the system instruction SHALL be composed in fixed order from: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md`, `USER.md`, `BOOTSTRAP.md`. The first three and the last SHALL be read from the agent's workspace directory. `WORKSPACE.md` SHALL be rendered from the workspace record (name, description) plus a `## Shared memory` subsection carrying the workspace's shared memory content. `USER.md` SHALL be rendered from the calling user's record and workspace membership (name, email, role) plus a `## Memory` subsection carrying that user's own memory content. The memory subsections are distinct from the structured metadata (which remains free context): they carry preferences and information the structured fields do not capture. Composition SHALL happen per execution because `USER.md` varies by caller and memory may have changed since the previous turn, and missing documents and empty memory SHALL be skipped without failing the run (an empty memory omits its subsection entirely).

#### Scenario: Fixed document order
- **WHEN** an execution composes its instruction with all six documents present
- **THEN** the system instruction contains their contents in the order AGENTS, IDENTITY, SOUL, WORKSPACE, USER, BOOTSTRAP

#### Scenario: USER.md varies by caller
- **WHEN** two different members execute the same agent
- **THEN** each execution's instruction carries that member's own name, email, and workspace role

#### Scenario: Missing documents tolerated
- **WHEN** an agent's prompt generation failed and IDENTITY.md/SOUL.md/BOOTSTRAP.md are absent
- **THEN** the execution still runs with the remaining documents (at minimum AGENTS.md plus the two virtual documents)

#### Scenario: Memory rides every turn
- **WHEN** an agent appends to `WORKSPACE.md` during one turn and executes a second turn
- **THEN** the second turn's composed instruction already contains the appended text under the `## Shared memory` subsection

#### Scenario: Empty memory omitted
- **WHEN** a user and workspace have no stored memory
- **THEN** the composed instruction carries the metadata docs without any memory subsections
### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep, delete) SHALL operate only on paths inside the agent's workspace directory; any resolved path escaping it SHALL be rejected as a tool error, not a crash. The agent's generated prompt documents, its agent-tier skills directory, and the summarization offload file all live inside this directory and are reachable through the file tools, including deletion of prompt documents such as `BOOTSTRAP.md`. (Shell execution is no longer banned outright — it is governed by the "Shell execution" and "Dangerous-command approval" requirements below.)

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

#### Scenario: Delete within the jail
- **WHEN** a file tool deletes a document inside the agent's workspace directory
- **THEN** the file is removed; deleting a missing file is an error result, not a crash
### Requirement: Three-tier skills
Skills SHALL be discovered from three sources: the system tier (`<ONCLAW_DIR>/skills`, embedded and mirrored at startup), the workspace tier (`<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/<name>/` where each skill carries a registry row), and the agent tier (`<ONCLAW_DIR>/workspaces/<tenant_slug>/agents/<agent_slug>/skills`). Attachment SHALL be governed by tier rules with no per-agent skill denylist: system-tier skills SHALL attach to every agent always; a workspace-tier skill SHALL attach to every agent in the workspace when its registry row is `enabled` and to no agent when disabled; agent-tier skills SHALL attach to their owning agent only. There is no per-agent skill toggle at any tier. On name collision the most specific tier SHALL win: agent > workspace > system. Skills SHALL be consumed through progressive disclosure: metadata lists first, full SKILL.md bodies fetched on demand.

#### Scenario: System skills cannot be disabled
- **WHEN** any actor attempts to disable a system-tier skill
- **THEN** no operation or affordance exists; the skill remains attached to all agents

#### Scenario: Master switch governs workspace skills
- **WHEN** a workspace skill's registry row is disabled
- **THEN** no agent in that workspace attaches the skill until the row is re-enabled

#### Scenario: Agent authors its own skill
- **WHEN** an agent writes a new SKILL.md document into its own skills directory via a file tool
- **THEN** the skill is discoverable in a subsequent execution of that agent

#### Scenario: Collision precedence
- **WHEN** a skill name exists in both the workspace tier and the agent tier
- **THEN** the agent-tier document is the one served

#### Scenario: System skills synced at startup
- **WHEN** the server starts
- **THEN** the embedded system skill set is mirrored into `<ONCLAW_DIR>/skills` — changed files overwritten, files not in the embedded set removed

### Requirement: Explicit skill mentions
For every execution surface, user input SHALL be scanned for `$name` tokens matching an available skill (system, enabled workspace, or owning-agent tier). A match SHALL turn into a blocking instruction — injected ahead of the user's message — directing the agent to invoke the skill tool for that name before producing any other response; execution SHALL still flow through the skill middleware's tool. Non-matching `$` tokens SHALL pass through as ordinary text.

#### Scenario: Mention forces skill load
- **WHEN** an execution's input contains `$web-research` and the skill is available
- **THEN** the agent invokes the skill tool for `web-research` before generating any other response about the task

#### Scenario: Disabled skill mention is plain text
- **WHEN** an execution's input contains the name of a workspace skill whose registry row is disabled
- **THEN** no blocking instruction is injected and the token is ordinary text

### Requirement: Workspace skill reachability in the jail
The filesystem jail SHALL grant read-only access to the workspace skills directory tree (`<ONCLAW_DIR>/workspaces/<tenant_slug>/skills`) in addition to the agent's own directory: file tools SHALL resolve absolute paths under either root, reads and greps SHALL succeed, and write/edit SHALL remain restricted to the agent's own directory. Symlink escape checking SHALL apply per root. Shell executions SHALL resolve skill runtime environments through PATH precedence: when a workspace skill venv exists, its bin directory SHALL precede the default PATH for shell commands in that workspace.

#### Scenario: Bundled file readable by absolute path
- **WHEN** an agent reads `<ONCLAW_DIR>/workspaces/<tenant_slug>/skills/pdf-toolkit/references/usage.md` via a file tool
- **THEN** the read succeeds

#### Scenario: Write into workspace skills rejected
- **WHEN** an agent attempts to write or edit any file under the workspace skills directory
- **THEN** the tool returns an error and no file is modified

#### Scenario: Symlink escape still rejected
- **WHEN** a file tool resolves a symlink under the workspace skills root pointing outside both roots
- **THEN** the operation is rejected as a path escape

#### Scenario: Venv python resolves first
- **WHEN** a shell command runs `python3` in a workspace whose skills venv exists
- **THEN** the venv's python interpreter executes and its installed packages are importable

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
Conversations SHALL be persisted as an append-only, totally ordered event log per thread, independent of any particular model message format. The log SHALL support: cursor pagination (after a given event id, with limit, optionally newest-first), filtering by event kind, and idempotent appends — appending an event whose identity already exists in the thread SHALL NOT duplicate it. A thread's current message window SHALL be reconstructible by replaying the log in order, including across summarization replacements. Execution checkpoints SHALL persist under a resolvable id. Every history query SHALL be workspace-scoped; events from one workspace SHALL be unreachable from another. A workspace member SHALL be able to read a session's persisted transcript as UI-shaped transcript events through the agents API, ordered by log sequence, with event-id cursor pagination, projected with full call fidelity: tool-call started events carry the call's arguments, tool-call finished events carry the call's result, error flag, and measured latency, and completed assistant messages carry the reasoning content persisted with them; the read path SHALL NOT alter how the model receives context, which remains full per-session replay (no provider response-id chaining).

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

#### Scenario: Transcript read returns UI-shaped events
- **WHEN** a workspace member GETs a session's events endpoint for a thread with persisted messages and tool calls
- **THEN** the response contains transcript events in log order — user and assistant messages as completed-message events, tool calls as started/finished events, compaction as a context-compacted event — each carrying its turn id and occurrence time, plus a next-cursor when more pages remain

#### Scenario: Transcript read carries call fidelity
- **WHEN** a persisted session holds an assistant message that requested tool calls and the tool results that followed them
- **THEN** the transcript read projects the started events with each call's arguments, the finished events with each call's result, error flag, and measured latency, and assistant messages with their persisted reasoning content — the same fields the live stream delivered

#### Scenario: Transcript read is cursor-paginated
- **WHEN** the read endpoint is called with `after` set to an event id and a `limit`
- **THEN** only events after that id are returned in a single page with the next cursor, and an unknown or out-of-range cursor yields an empty page rather than an error

#### Scenario: Read path leaves model context alone
- **WHEN** a subsequent chat turn executes on a thread after any number of transcript reads
- **THEN** the model still receives the full replayed session history from the event log, and no provider response-id chaining is introduced
### Requirement: Cancellation at a safe point
A running execution SHALL be cancellable by an explicit cancel request addressing the run (workspace, agent, session). Cancellation SHALL take effect at a safe point — the in-flight model call or tool call either completes and is recorded, or is aborted with its partial state marked — and SHALL record a cancel marker in the session history. Cancellation SHALL NOT leave dangling tool-call records that would break the next execution on the thread. A consumer disconnecting — an HTTP request returning, a stream client closing, or a stream never being consumed — SHALL NOT cancel the run.

#### Scenario: Cancel between tool calls
- **WHEN** a caller cancels while tool calls are executing
- **THEN** in-flight calls finish or abort cleanly, a cancel marker is recorded, and a subsequent execution on the thread starts from a consistent history

#### Scenario: Consumer disconnect does not cancel
- **WHEN** the client that started a turn closes its connection before the turn completes
- **THEN** the run continues to completion and its events are persisted to session history

#### Scenario: Explicit cancel endpoint
- **WHEN** an authorized member cancels the active run of a session
- **THEN** the run stops at the next safe point with a cancel marker, and the cancel endpoint reports the run as cancelled

### Requirement: Detached execution lifetime
An execution's context SHALL derive from the server's base context, not from any request or stream context. Completing the request that started a run, or ending any consumer's connection, SHALL NOT terminate or pause the run. The server SHALL track live runs so they can be cancelled explicitly and drained on graceful shutdown.

#### Scenario: Approval resume outlives its request
- **WHEN** an approval-resolution request returns its response before the resumed turn completes
- **THEN** the resumed turn continues running and its events reach session history

#### Scenario: Fire-and-forget start
- **WHEN** a caller starts a run and abandons the returned stream without consuming it
- **THEN** the run completes and persists its transcript

#### Scenario: Graceful shutdown drains runs
- **WHEN** the server shuts down while runs are in flight
- **THEN** in-flight runs get a bounded window to reach a terminal state, and any run that does not drain is cancelled with a cancel marker

### Requirement: Non-blocking event tap
The live event stream SHALL behave as a tap supporting multiple concurrent subscriber consumers per active run, not a 1:1 dedicated pipeline: when no consumer drains a tap, or when a slow consumer leaves its buffer full, the runner SHALL drop further tap events for that subscriber rather than block. When a new consumer connects or re-connects to an in-flight run, the system SHALL attach a fresh live subscriber tap to the executing run. Persisted history SHALL remain complete regardless of tap drops or consumer disconnections, and a consumer MAY recover dropped or missed events from history by cursor (?after=).

#### Scenario: Unwatched run does not stall
- **WHEN** a run's events exceed the tap buffer and no consumer is reading
- **THEN** excess tap events are dropped, and the run itself proceeds and persists normally

#### Scenario: History recovers dropped tap events
- **WHEN** a consumer that missed tap events polls history with the last event it saw
- **THEN** it receives every persisted event after that cursor, including any the tap dropped

#### Scenario: Multiple concurrent consumers receive live stream
- **WHEN** a second client or reconnected stream subscribes to an active executing run
- **THEN** both subscribers receive subsequent live transcript events as they occur without stalling the execution loop
### Requirement: Bounded execution loop
An agent execution's reasoning loop SHALL be bounded by a server-configured maximum number of model iterations. When a turn would exceed the bound — the model keeps requesting tool calls without reaching a final answer — the runtime SHALL terminate the turn with an error outcome (surfaced as the terminal error transcript event) instead of continuing indefinitely. Normal turns that conclude within the bound SHALL be unaffected.

#### Scenario: Runaway loop terminates
- **WHEN** an agent's model keeps issuing tool calls beyond the maximum iteration count without producing a final answer
- **THEN** the turn ends with the terminal error transcript event and no further model calls are made

#### Scenario: Converging turn unaffected
- **WHEN** a turn reaches its final answer within the maximum iteration count
- **THEN** the execution completes normally with a turn-completed terminal event

### Requirement: Tool selection
An agent's tool surface SHALL be resolved from its `tools` allowlist intersected with the workspace's enabled tool set (see the workspace-tools capability — the workspace gate wins over the allowlist and over per-turn allowed-tools overrides). A name listed in `tools` SHALL expose the corresponding registered built-in tool. The reserved name `execute` SHALL enable the shell tool (see "Shell execution"). The facade alias `browser` SHALL expand to the full browser tool set at resolution (see "Browser automation"); individual `browser.*` names in an allowlist SHALL continue to resolve for backward compatibility. The filesystem middleware tools SHALL be selectable through the allowlist names `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`: a name present SHALL keep that middleware tool attached, a name absent SHALL disable it via the filesystem middleware's per-tool disable configuration. An agent whose `tools` array is empty SHALL have no registry tools and no filesystem tools exposed. Names in `tools` that match no registered tool SHALL be ignored, not errors. Built-in tools registered after an agent's allowlist was saved SHALL NOT appear for that agent until its allowlist is updated.

#### Scenario: Allowlist gates the surface
- **WHEN** an agent's `tools` contains only `web.search` and the workspace enables it
- **THEN** its executions expose `web.search` and no other registry tool

#### Scenario: Filesystem tools follow the allowlist
- **WHEN** an agent's `tools` contains `read_file` and `glob` but not `write_file`
- **THEN** its executions expose the read and glob file tools and no write tool

#### Scenario: Empty allowlist exposes nothing
- **WHEN** an agent has an empty `tools` array
- **THEN** no registry tool and no filesystem tool is available to its executions

#### Scenario: Facade alias expands
- **WHEN** an agent's `tools` contains `browser` and the workspace enables it
- **THEN** its executions expose the full current browser tool set

#### Scenario: Legacy individual browser names still resolve
- **WHEN** an existing agent's `tools` contains `browser.navigate` and `browser.read`
- **THEN** exactly those tools resolve until the allowlist is updated

#### Scenario: Workspace gate intersects
- **WHEN** the workspace disables `web.search` and an agent's allowlist contains it
- **THEN** the agent's executions expose no `web.search` tool

#### Scenario: Unknown names are inert
- **WHEN** an agent is saved with `tools` naming a tool no registry provides
- **THEN** the save succeeds; the unknown name is inert at execution time

#### Scenario: Later-registered tools do not leak
- **WHEN** a new built-in tool is registered after an agent's allowlist was saved
- **THEN** the agent's executions do not expose it until the allowlist names it

### Requirement: Shell execution
When the agent's `tools` allowlist contains the reserved name `execute`, the runtime SHALL expose a shell tool. Shell commands SHALL execute with the agent's workspace directory as the working directory, SHALL inherit a scrubbed minimal environment (no instance secrets), and SHALL be bounded by a timeout and an output cap, with truncation and exit code reported in the tool result. Agents whose allowlist omits `execute` SHALL have no shell tool. The workspace jail SHALL be understood as a working-directory convention, not an OS sandbox.

#### Scenario: Allow-listed shell runs in the jail
- **WHEN** an agent with `execute` in `tools` runs a shell command that writes a file
- **THEN** the command executes with the agent's workspace directory as its working directory and the file appears inside the jail

#### Scenario: Shell not allow-listed
- **WHEN** an execution's agent does not list `execute` in `tools`
- **THEN** no shell tool is present on its tool surface

#### Scenario: Timeout bounds the command
- **WHEN** a shell command runs longer than the configured timeout
- **THEN** the command is stopped and the tool result reports the timeout with whatever output was produced

### Requirement: Dangerous-command approval
The shell tool SHALL classify each command against a built-in dangerous-pattern list (destructive filesystem operations, piping network fetches into a shell, privilege escalation, disk/format utilities, host power/reboot, and similar). A matching command SHALL NOT execute immediately: the runtime SHALL pause the execution, persist an `approval_required` transcript event carrying the command and an interrupt identifier, and wait for a human decision. A pending approval SHALL remain resolvable across server restarts. An approve decision SHALL execute the command and record its output as the tool result; a deny decision SHALL record a denial notice as the tool result without executing. Resolution SHALL be possible through an authenticated endpoint permitted to workspace members.

#### Scenario: Dangerous command pauses for approval
- **WHEN** an allow-listed shell tool is asked to run a command matching the dangerous list
- **THEN** the command does not execute, an `approval_required` event is recorded in the transcript, and the turn pauses

#### Scenario: Approval executes the command
- **WHEN** a member approves a pending approval
- **THEN** the command executes and its output appears in the transcript as the tool result

#### Scenario: Denial records a refusal
- **WHEN** a member denies a pending approval
- **THEN** the command never runs and the transcript records a denial notice as the tool result

#### Scenario: Approval survives a restart
- **WHEN** the server restarts while an approval is pending
- **THEN** the approval can still be resolved afterwards and the execution resumes from its checkpoint

### Requirement: Web fetch tool
The runtime SHALL expose a `web.fetch` tool that retrieves an http(s) URL and returns its readable text content, size-capped with an explicit truncation marker. The fetcher SHALL deny loopback, private, and link-local addresses by default — re-validating at each redirect hop — with an instance configuration flag to opt out for self-hosted internal use.

#### Scenario: Public page returns readable text
- **WHEN** the tool fetches a public HTML page
- **THEN** the result carries the page's readable text, truncated with a marker when over the size cap

#### Scenario: Internal address denied by default
- **WHEN** the tool is asked to fetch a loopback, private, or link-local URL
- **THEN** the fetch is refused with an error naming the egress guard

#### Scenario: Redirect re-validation
- **WHEN** a public URL redirects to a private address
- **THEN** the redirected fetch is refused under the same guard

### Requirement: Web search tool providers
The `web.search` tool SHALL resolve queries through a search provider selected per workspace from the workspace's tool settings, falling back to instance configuration when the workspace has none, and to the zero-credential DuckDuckGo backend when neither exists (see the workspace-tools capability, "Search provider configuration"). The provider registry SHALL be extensible: a provider registers the credential kind it requires (`none`, `api_key`, `base_url`) and the catalog uses this to render configuration. The result shape SHALL be identical across providers. A workspace-selected provider whose credential is missing SHALL fail the tool's construction for the execution with an error naming the missing configuration, not silently fall back.

#### Scenario: Workspace provider selected
- **WHEN** the workspace configures `brave` with an API key
- **THEN** `web.search` in that workspace resolves through Brave in the standard result shape

#### Scenario: Instance fallback preserved
- **WHEN** the workspace has no search settings and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily

#### Scenario: Zero-credential default
- **WHEN** neither workspace nor instance configuration exists
- **THEN** `web.search` uses the DuckDuckGo backend

#### Scenario: Misconfigured workspace provider fails construction
- **WHEN** the workspace selects `tavily` without a stored key
- **THEN** the tool construction for the execution fails with an error naming the missing configuration

### Requirement: Browser automation
The runtime SHALL expose a browser tool set behind the `browser` facade: `browser.navigate`, `browser.snapshot`, `browser.click`, `browser.type`, `browser.hover`, `browser.drag`, `browser.select_option`, `browser.act`, `browser.read`, and `browser.screenshot`. The set SHALL follow the accessibility-snapshot model: `browser.snapshot` SHALL capture a page snapshot whose elements carry stable references, and the interaction tools (`click`, `type`, `hover`, `drag`, `select_option`) SHALL target elements by those references. The runtime SHALL attach to the workspace-configured remote CDP endpoint when one is provided, otherwise launch a local Chromium under the workspace-configured headless setting, honoring `max_pages`, `idle_timeout_seconds`, and `action_timeout_seconds` from the workspace's tool settings (see the workspace-tools capability, "Browser tool configuration"). When no browser is available, the tools SHALL return a clear availability error instead of failing the run. Each execution SHALL get its own isolated browser session, torn down when the execution ends or the idle timeout expires. Screenshots SHALL be written into the agent's workspace directory, and the tool result SHALL return the in-jail path.

#### Scenario: Snapshot then ref-targeted interaction
- **WHEN** the tools take a snapshot and then click an element by its snapshot reference
- **THEN** `browser.click` operates on that element and returns a fresh snapshot in its result

#### Scenario: Navigate and read a page
- **WHEN** the tools navigate to a URL and then read the page
- **THEN** `browser.navigate` reports the destination (title/status) and `browser.read` returns the page's readable content

#### Scenario: Session is per-execution
- **WHEN** an execution ends
- **THEN** its browser session and any launched browser process are stopped, and a later execution starts a fresh session

#### Scenario: Screenshot lands in the workspace
- **WHEN** `browser.screenshot` captures the current page
- **THEN** the PNG is written inside the agent's workspace directory and the result returns that path

#### Scenario: No browser available
- **WHEN** no CDP endpoint is configured and no local Chromium can be found
- **THEN** browser tool calls return an availability error describing the configuration needed

### Requirement: Per-turn usage capture
The runtime SHALL capture token usage per execution turn — input tokens, output tokens, and their total, as reported by the model provider — and SHALL expose the totals on the turn's terminal transcript event. Usage SHALL be captured whether or not any consumer is attached to the live stream, and SHALL persist with the turn's history. The capture SHALL additionally record the **final-call input tokens**: the input token count of the turn's last model call, representing the context size the model last saw. The plain input total accumulates every model call in the turn, so on a multi-call turn it exceeds the final-call count; both are reported independently.

#### Scenario: Usage reported at turn end
- **WHEN** an execution reaches its terminal event
- **THEN** the terminal event carries the turn's input/output/total token counts as reported by the provider

#### Scenario: Usage persists with history
- **WHEN** a turn's transcript is reloaded from persisted history
- **THEN** the turn's usage totals are retrievable from durable data

#### Scenario: Final-call input diverges from accumulated total
- **WHEN** a turn makes three model calls whose provider-reported inputs are 40,000, 45,000, and 50,000 tokens
- **THEN** the turn's usage reports an input total of 135,000 and final-call input of 50,000

#### Scenario: Single-call turn matches
- **WHEN** a turn makes exactly one model call whose provider-reported input is 1,200 tokens
- **THEN** the turn's usage reports input total 1,200 and final-call input 1,200

#### Scenario: Final-call input survives reload
- **WHEN** a multi-call turn's transcript is reloaded from persisted history
- **THEN** the turn's final-call input count is rebuilt from the persisted record and equals the last model call's input

### Requirement: MCP tool resolution
An agent's execution SHALL expose MCP tools in addition to the built-in surface resolved from its `tools` allowlist: the tools of every workspace MCP server whose id appears in the agent's `enabled_mcps` while that server is enabled at the workspace level, plus the tools of the agent's private MCP servers. MCP tool exposure SHALL be independent of the `tools` allowlist, per-turn allowed-tools overrides, and the workspace tool gate — it is governed solely by the opt-in allowlist, the workspace master switch (see the workspace-mcp capability), and private server attachment. MCP tools SHALL surface under names of the form `mcp__<server>__<tool>` sanitized to provider-safe characters; a name collision after sanitization SHALL be resolved by suffixing so every tool remains individually addressable. A server that cannot be contacted or fails tool listing at execution SHALL contribute no tools, SHALL have its stored status moved to `error` with the failure message, and SHALL NOT fail the run. Tool selection SHALL be at server granularity — no per-tool filtering within a server.

#### Scenario: Opted-in server contributes tools
- **WHEN** an agent's `enabled_mcps` contains a connected, enabled server named "github" exposing `create_issue`
- **THEN** the agent's executions include a tool addressable as `mcp__github__create_issue` alongside its built-in tools

#### Scenario: Empty opt-in exposes nothing
- **WHEN** an agent's `enabled_mcps` is empty and it has no private servers
- **THEN** its executions expose no MCP tools, regardless of what the workspace has registered

#### Scenario: Workspace master switch wins
- **WHEN** an agent has opted into a server that is paused at the workspace level
- **THEN** none of that server's tools appear in the agent's executions

#### Scenario: Private server appears only for its agent
- **WHEN** agent A carries a private server and both agents execute
- **THEN** only agent A's executions include that server's tools

#### Scenario: MCP tools ignore the tools allowlist
- **WHEN** an agent's `tools` array is empty but its `enabled_mcps` contains a connected server
- **THEN** its executions expose no built-in registry tools but do expose the server's MCP tools

#### Scenario: Dead server degrades gracefully
- **WHEN** an execution resolves tools and one opted-in server cannot be contacted
- **THEN** the run proceeds with the remaining tools, the server's stored status becomes `error` with the failure message, and no error surfaces to the conversation on that server's account

#### Scenario: Names survive sanitization
- **WHEN** a server named "My Server" exposes a tool `do.thing` and another server exposes a tool sanitizing to the same name
- **THEN** both tools are exposed with distinct, provider-safe `mcp__` names

### Requirement: Birth ritual completion
`BOOTSTRAP.md` SHALL be composed into the instruction only while the file exists in the agent's workspace directory. The agent SHALL be able to remove it through the file tools (deleting `BOOTSTRAP.md` completes the birth sequence described in the document), and removal SHALL be permanent — the file is seeded only when a new agent is created, never re-created afterward.

#### Scenario: Agent completes the birth sequence
- **WHEN** an agent deletes `BOOTSTRAP.md` via a file tool during its first conversation
- **THEN** the deletion succeeds and subsequent executions compose their instruction without the bootstrap document

### Requirement: Streaming session event catch-up and live switchover
The session events endpoint SHALL support server-sent event (SSE) streaming with cursor pagination (`after`). When a client requests streaming events for a session, the endpoint SHALL first stream all persisted events occurring after the given cursor in chronological order. If a live run is currently active for that session, the endpoint SHALL seamlessly attach to the live run's event broadcast and stream subsequent events until the run reaches a terminal event (completed, error, or cancelled) and emit a terminal `[DONE]` marker. If no run is active, the endpoint SHALL close after replaying the persisted history.

#### Scenario: Catch-up on finished run
- **WHEN** a client connects with an `after` cursor to a session whose run is already completed
- **THEN** the server streams all persisted events after the cursor and immediately terminates the stream with a completed state

#### Scenario: Re-attach and live switchover on active run
- **WHEN** a client connects with an `after` cursor to a session while an agent execution is actively running
- **THEN** the server streams historical events past the cursor, transitions to streaming live execution events as they are produced, and terminates when the active run completes
