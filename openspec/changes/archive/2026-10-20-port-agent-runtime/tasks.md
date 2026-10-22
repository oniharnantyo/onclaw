# Tasks

## 1. W1 — Tool execution loop, jail, shell, todos, web tools

- [x] 1.1 Spike and port the compose core: `Config`/`Compose` building the ADK agent with the tool loop behind the unchanged `Manager` interface, with fake-model unit tests covering tool call → result → final answer, the iteration cap notice, and tool-failure feedback in `internal/agents`
- [x] 1.2 Extend the sessions event model additively (`ToolActivity.Status` running/completed/failed + real `Result`, live start/finish emit, terminal record persisted, replay tolerates unknown kinds) and verify with transcript-replay and persistence tests in `internal/sessions` + `internal/store/sqlite`
- [x] 1.3 Add migration `000007_runtime_port.sql` (`agents.subagents_enabled`, `agent_hooks`, `hook_executions`, `tool_settings`) with store interfaces, SQLite impls, and fakes, updating the schema-version tests that pin the migration count; verify with `go test ./internal/store/...`
- [x] 1.4 Port the filesystem jail (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `delete_file` over `workspaces/<slug>` as the workspace root) and verify with unit tests for each tool plus in-jail success and escape-rejection cases
- [x] 1.5 Port the jailed `execute` shell (workspace cwd, bounded timeout, output cap, scrubbed child env) and verify with unit tests for output capture, timeout kill, and env scrubbing
- [x] 1.6 Implement the todo tools persisting the plan in the agent's workspace and verify with a cross-turn persist test
- [x] 1.7 Port `web_fetch` and the `web_search` provider seam (brave/exa/perplexity/firecrawl/searxng + failover chain, tool_settings-backed config) and verify with fake-HTTP-client unit tests for fetch, search, failover, and the no-provider error result
- [x] 1.8 Wire the tool gate (catalog × global enable × agent allowlist) into resolve and verify with gate tests covering disabled-global, narrowed-allowlist, and empty-allows-all
- [x] 1.9 Add the Tools-pane web_search chain section (provider catalog with per-provider credential requirements, ordered chain, persistence via the settings service) and verify with component tests plus regenerated bindings after `task build`
- [x] 1.10 Upgrade `ToolCallCard` to live running/completed/failed states from the extended events and verify with component tests in `frontend/src/features/chat/__test__`
- [x] 1.11 Run the wave gates (`gofmt -w .`, `go vet ./...`, `go test ./...`, `vp check` on `frontend/src`, `npm run build` + `task build` + bindings regen) and a temp-HOME live smoke where an agent writes a file through the jailed fs tool and runs a shell command against the mock provider

## 2. W2 — MCP client

- [x] 2.1 Port the MCP connection manager (lazy connections, connect timeout, negative-cache retry, janitor, invalidate-on-write) with stdio/streamable-HTTP/SSE transports and verify with unit tests against an in-process MCP stub for each transport
- [x] 2.2 Implement probe + tool discovery writing the existing status/status_error/tool_count fields, exposed through the settings service (probe call, unchanged definition on failure), and verify with service tests plus regenerated bindings
- [x] 2.3 Wire `mcp__<server>__<tool>` naming with sanitization/collision suffixes and the opt-in policy (enabled server + agent opt-in; bypasses the tool allowlist) into resolve, and verify with runner tests using a fake connection for offer, non-offer, and mid-turn failure cases
- [x] 2.4 Upgrade the MCP pane with live state (probe affordance, connected + tool count, failed + reason) and verify with component tests in both themes' class matrix
- [x] 2.5 Run the wave gates and a temp-HOME live smoke where an agent opts in to a local stdio stub server and round-trips one MCP tool call

## 3. W3 — Skills into turns

- [x] 3.1 Port the two-tier skill backend (global `~/.buddies/skills` + `workspaces/<slug>/skills`, agent tier wins, fresh read per turn) and verify with unit tests for discovery, precedence, and live pickup of a newly added workspace skill
- [x] 3.2 Wire progressive disclosure (metadata-only in context, body on demand) and `$name` invocation preprocessing into the turn pipeline and verify with runner tests for idle-metadata context, on-demand body load, and explicit invocation
- [x] 3.3 Apply the eligibility gate (global enable + agent skill allowlist, empty = all) in resolve and verify with gate tests including the disabled-skill case across metadata and `$`-invocation
- [x] 3.4 Extend the workspaces manager for the `skills/` and `.tasks/` layout (created on demand) and verify with manager unit tests
- [x] 3.5 Run the wave gates and a temp-HOME live smoke where a workspace skill's body is loaded during a turn via `$name`

## 4. W4 — Hooks dispatch

- [x] 4.1 Port the hook dispatcher (five events, matcher, ordered resolution: assigned hooks then unassigned globals) with the allow/block decision contract and verify with dispatcher unit tests for ordering, matcher skip, and decision parsing
- [x] 4.2 Implement the command handler (event JSON on stdin, bounded timeout, exit-code/JSON decision) and the script handler (goja, console-only bindings, interrupt timeout) and verify with handler unit tests including block decisions, timeouts, and sandbox containment
- [x] 4.3 Implement `agent_hooks` assignment CRUD and `hook_executions` audit through the store (impls + fakes, cascade on agent/hook delete) and verify with store tests
- [x] 4.4 Wire dispatch into the turn pipeline at all five points with audit writes (blocked prompt makes no model call; blocked tool never executes; failing hook never crashes the turn) and verify with runner tests per point
- [x] 4.5 Update the services layer: Hooks pane limited to command/script types, `TestHook` generalized to a sample-event dry run for both types, assignment API for agent config; verify with service tests plus regenerated bindings
- [x] 4.6 Replace the informational hooks section in `AgentConfigModal` with real assignment UI (pick, order, remove) and delete the `hooksInfo` stub copy, verifying with component tests
- [x] 4.7 Add the hook-execution audit view to the Hooks pane and verify with a component test
- [x] 4.8 Run the wave gates and a temp-HOME live smoke where a command hook blocks one tool call end-to-end with the reason visible in the transcript

## 5. W5 — Subagents

- [x] 5.1 Surface `agents.subagents_enabled` through the store, agent DTOs, and bindings (default on) and verify with store + service tests
- [x] 5.2 Implement the `agent` tool delegating to a general-purpose clone (parent persona/model/tools, fresh context, no recursion) and verify with runner tests for the delegation round trip and the clone's tool surface
- [x] 5.3 Implement the background lane (`run_in_background`, `task_output`, `task_stop`, per-run task-ID space) and verify with runner tests for parent-continues, output collection, and cancellation
- [x] 5.4 Materialize child transcripts under `workspaces/<slug>/.tasks/` and verify with a test that the parent's file tools can read a finished child transcript
- [x] 5.5 Add the `task_activity` event kind (live + persisted) and render background task cards (running, completion/cancellation, output access) in the chat view, verifying with persistence tests and component tests
- [x] 5.6 Add the subagents toggle to agent configuration and verify with a component test plus regenerated bindings
- [x] 5.7 Run the wave gates and a temp-HOME live smoke covering a foreground delegation, a background delegation with output collection, and a task_stop

## 6. Final integration

- [x] 6.1 Run the full gates across the repo (`gofmt -w .`, `go vet ./...`, `go test ./...`, `vp check` scoped to `frontend/src`, `npm run build` then `task build` with bindings regen verified) and confirm green
- [x] 6.2 Exercise the new UI (tool cards, task cards, MCP pane, Tools-pane search section, hooks assignment, subagents toggle) in both themes via dev-run/IAB screenshots and fix any legibility gaps
- [x] 6.3 Run a server-mode headless smoke (`bin/buddies-server`, temp HOME, mock provider + MCP stub + command hook) exercising one turn that uses a jailed file tool, an MCP tool, and a blocking hook, and confirm the transcript persists correctly
