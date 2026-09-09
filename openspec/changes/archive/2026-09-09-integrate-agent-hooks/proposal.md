## Why

Agents run tool calls and turns with no policy layer between the model and the world: the only gates today are the workspace tool gate (static allowlist) and the shell approval flow (human-in-the-loop). Teams self-hosting OnClaw need automation-grade control at agent lifecycle points — notify on failures, audit cron activity, block destructive commands during a freeze, gate risky tools through an LLM policy check — without editing agents or shipping code. Every comparable harness (Claude Code, GoClaw, OpenClaw, Hermes) has converged on lifecycle hooks as the answer; OnClaw's runtime already has the seams (run entry, eino tool middleware chain, terminal handling) and the precedents (MCP servers for scoped capability config, skills for tier governance).

## What Changes

- Add an **agent hooks** capability: workspace-defined handlers that fire at five lifecycle events — `run_started`, `user_prompt_submit` (blocking), `pre_tool_use` (blocking), `post_tool_use`, `run_finished` (all terminal outcomes, with `status`).
- Blocking hooks decide via a uniform contract — `{"decision": "allow"|"block", "reason"}` — delivered as an HTTP response body, command stdout, MCP tool text result, or a forced `decide()` tool call; a block feeds the model a readable tool-result payload (never a run failure, never the approval interrupt). First block wins.
- Four handler types registered behind one interface: `http` (webhook), `command` (exec-form process, stdin JSON, exit-2-compatible with the Claude Code ecosystem), `mcp_tool` (invoke a workspace MCP server tool with `${event.*}` placeholders), `prompt` (sandboxed LLM evaluator with structured `decide()` output, per-run invocation cap, required matcher).
- Event-aware **matcher**: a structured tool/origin/status picker (`all` / `all except` / `only`) or an advanced unanchored RE2 pattern, validated at save with a match-count report.
- **Three-level scope** following the skills governance model: instance-level hooks (mandatory, superadmin surface: release-shipped builtins re-synced at startup + superadmin-managed definitions), workspace-level hooks (always-on for all agents in the workspace, per-hook enabled switch), agent-level hooks (private to one agent). Evaluation order instance → workspace → agent, list order within a level; the numeric priority concept is dropped — order is the list.
- `hook_executions` audit log (survives hook deletion), per-hook health status, transcript-visible hook blocks, and a dry-run **Test** action per hook.
- Workspace settings gains a Hooks pane; the agent config modal gains agent-level hooks CRUD plus read-only visibility of instance/workspace hooks; chat transcripts render hook blocks and notices.

## Capabilities

### New Capabilities

- `agent-hooks`: the hooks capability end to end — event catalog and seams, dispatcher and decision semantics, event-aware matcher, the four handler types and their contracts, three-level scope with instance builtin sync (including multi-instance behavior), audit and health surfacing, permissions, and the REST API.

### Modified Capabilities

- `web-app/settings`: new Hooks pane — list with drag ordering and health status, create/edit dialog (event → applies-to → handler section → failure policy), Test panel, execution history view.
- `web-app/agents`: agent configuration modal gains a Hooks section — agent-level hook CRUD plus read-only visibility of instance and workspace hooks that reach the agent.
- `web-app/chat`: transcript renders hook-enforced blocks (blocked tool calls show the hook's reason) and blocked-prompt notices.

## Impact

- **Backend**: new `internal/agents/hooks` package (dispatcher, matcher, handler registry); eino middleware appended in `internal/agents/agent.go` `buildMiddlewares`; run-entry and terminal check points in `internal/agents/runner.go`; new store sub-interface + postgres implementation + `internal/bootstrap` builtin sync; REST handlers under `internal/server/handlers`; permission catalog additions (`hooks.read`, `hooks.write`) with a built-in-role backfill migration.
- **Schema**: new migration — `instance_hooks` (the one deliberately workspace-unscoped table), `workspace_hooks`, `agent_hooks`, `hook_executions`; `agents` table gains no hook columns.
- **Frontend**: workspace settings Hooks pane, agent config modal Hooks section, chat transcript hook rendering; reuses MCP config components (secret rows, provider/model picker).
- **Ops**: `ONCLAW_HOOKS_COMMAND_ENABLED` instance flag (default on) gating the command handler; multi-instance deploys need no coordination (idempotent startup sync, per-run hook resolution, graceful skip of un-interpretable hooks).
