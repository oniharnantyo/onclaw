# Proposal

## Why

buddies' runtime declares tools to the model but never executes one: `runTurn` records `tool_activity` events with empty results and ends the turn. The four capability families around it — skills, MCP servers, hooks, subagents — exist only as dead configuration: tables, settings panes, and wizard steps that nothing reads at turn time. The predecessor project onclaw-v2 already built and hardened this exact stack on the same eino foundation; porting it makes buddies' agents actually agentic and switches the existing CRUD surfaces from decorative to load-bearing.

## What Changes

- Port onclaw-v2's ADK-based turn engine: the pure `Compose(Config)` middleware architecture replaces the hand-rolled streaming internals of `runTurn`. Turns execute tools and feed results back to the model, bounded by a max-iteration cap. Buddies' event-sourced sessions, compaction, and `Manager` surface stay; only the inside of the turn changes.
- Built-in tool catalog goes live: jailed filesystem tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `delete_file`) over the agent's own workspace directory, `execute` (jailed shell), `todo_write`/`todo_read`, plus the existing `current_time` and `http_get`; new `web_fetch` and `web_search` (provider seam: brave/exa/perplexity/firecrawl by API key, searxng by base URL, composed into a failover chain configured in the Tools pane).
- MCP servers become real clients: a connection manager speaking stdio, streamable HTTP, and SSE; on-demand probe with tool discovery and `mcp__<server>__<tool>` naming; per-agent opt-in via the existing `MCPServers` allowlist; discovered tools merged into the turn's tool surface; live connection state in the MCP pane.
- Skills become capability: two tiers — global `~/.buddies/skills/` and per-agent `workspaces/<slug>/skills/` (agent tier wins) — with `SKILL.md` progressive disclosure and `$name` invocation wired to the composer's existing skill menu; the per-agent allowlist is honored.
- Hooks dispatch during turns: all five events (`run_started`, `user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `run_finished`) with matchers and decision contract (allow/block), command (subprocess) and script (in-process JS) handler types, global hooks plus per-agent assignment, and an execution audit trail; the agent-config hooks section stops being informational copy.
- Subagents: the `agent` tool delegates to a general-purpose clone of the parent agent (same persona/model/tool surface, fresh context, no recursion), with an optional background lane (`run_in_background`, `task_output`, `task_stop`), child transcripts under `workspaces/<slug>/.tasks/`, and task progress visible in chat.
- Schema additions via forward-only migrations: per-agent hook assignments and per-tool settings (the search chain); the existing MCP status/tool-count columns become populated for real.
- Explicitly NOT ported from onclaw-v2: skill curation, MCP OAuth, the skill install pipeline (zip/git), browser tools, document tools, the hooks `http`/`prompt`/`mcp_tool` handler types, memory, and gateway triggers (channels/webhooks/cron stay out; the gateway remains the separate base seam).

## Capabilities

### New Capabilities

- `agent-tools`: the tool execution loop inside a turn (invoke → result → continue, bounded iterations), the built-in catalog with the workspace jail and jailed shell, and the web tools (web_fetch; web_search with its provider registry and failover chain).
- `agent-skills`: skills as a live capability — two-tier discovery (global + per-agent, agent wins), progressive disclosure of `SKILL.md` bodies, `$name` invocation, per-agent allowlist, disabled skills never offered.
- `agent-mcp`: the MCP client — connection manager, three transports, probe and tool discovery with namespaced naming, per-agent opt-in policy, merged tool surface, connection state surfaced to settings.
- `agent-hooks`: hook dispatch across the five turn events, command and script handler types, matchers and the allow/block decision contract, global + per-agent resolution order, execution audit.
- `agent-subagents`: delegation via the `agent` tool to a general-purpose clone, the background lane with task control tools, child transcript materialization under `.tasks/`, and task completion surfacing.

### Modified Capabilities

- `settings`: the Tools pane gains web_search provider chain configuration; the Hooks pane manages command/script hooks (handler-type form limited to the two supported types); the MCP pane shows live probed connection state with discovered tool counts.
- `agent-management`: agent configuration's hooks section becomes real per-agent hook assignment, and the capabilities gain the subagent toggle.
- `chat-view`: tool-call cards show live running/completed states with executed results, and background task progress is rendered in the transcript.
- `data-store`: the workspace layout gains `skills/` and `.tasks/` subdirectories, and the schema gains agent hook assignments and per-tool settings storage.

## Impact

- `internal/agents`: `runTurn` internals replaced by the ported resolve/compose/execute pipeline (eino ADK `ChatModelAgent` + middleware) behind the unchanged `Manager` interface and the package's no-eino-types-across-the-boundary rule; new subpackages ported/adapted from onclaw-v2 (`backend` fs jail + shell, hooks dispatcher, MCP manager); `builtin_tools.go` catalog expanded; `ToolActivity.Result` starts carrying real output.
- `internal/store` + `internal/store/sqlite`: new migrations (agent hook assignments, tool settings), new store interfaces + SQLite impls + fakes; the schema-version tests that pin the migration count need updating.
- `internal/services`: hooks service gains assignment + typed handlers; MCP service gains probe/discovery; skills service gains per-agent dirs; chat service gains task progress; Wails bindings regenerate after every build (`task build`), which the tasks treat as a standing gate.
- `frontend/`: real hooks assignment UI, MCP pane status/tool counts, Tools pane search-provider config, live tool-call card states, task progress rendering — all specified and checked in both themes; `vp check` stays the frontend gate.
- New Go dependencies: an in-process JS engine (goja) for script hooks and an MCP client library (eino-ext MCP tool component / mcp-go), mirroring onclaw-v2's choices.
- Verification load grows: each wave lands with unit tests plus a live smoke (mock provider, temp-HOME recipe) before the next starts.
