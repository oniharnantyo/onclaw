# Proposal

## Why

Workspace agents cannot delegate: every subtask — research, multi-step exploration, a long document build — lands in the delegating agent's own context window, and every tool call blocks the turn. Eino v0.10.0-alpha.35 ships the Claude-Code-style subagent orchestrator (`adk/middlewares/subagent`) and a process-local background-task lane (`adk/backgroundtask/local`) that solve exactly this; adopting them (and bumping eino from alpha.28 to alpha.35, where the surface is verified) gives agents a delegation tool with an optional `run_in_background` slot.

## What Changes

- Bump `github.com/cloudwego/eino` from v0.10.0-alpha.28 to v0.10.0-alpha.35 (verified additive for OnClaw: new `SetToolReturnDirectly` API, checkpoint-save/session-persistence error sentinels, parallel tool-result reordering on session reconstruction, summarization skill-preamble re-execution guard, retry `RewriteError` lane; no removed API OnClaw uses).
- Composition gains three inputs mirroring the eino deep-agent recipe: `SubAgents` (caller-supplied delegate-able agent instances), `Background` (process-local background delegation config), and `WithoutGeneralSubAgent` (disables the injected general-purpose clone). When the capability is wired, composition attaches the subagent middleware (delegation tool, instruction, available-types reminder) and the background control middleware (`task_output`, `task_stop`) in the fixed order.
- Unless disabled, composition injects a `general-purpose` subagent: a clone of the delegating agent (same instruction, model, tools, capability middlewares) with a fresh conversation context. Sub-agents carry no delegation capability themselves, so delegation cannot recurse.
- The runner wires the capability per agent via a reserved `subagents` allowlist name (same mechanism as the existing reserved `execute` shell name) plus a workspace tool-catalog row so the agent-config tool picker can toggle it. v1 exposes only the general-purpose subagent type.
- Background delegation (process-local): the delegation tool gains `run_in_background`; a background launch returns a task id immediately, the model can poll interim output with `task_output` and cancel with `task_stop`, and completion is pumped into the parent session as a transcript event. Tasks are process-local and die with the run or server restart — surfaced honestly in copy.
- Background shell: agents with the shell tool can additionally opt into running shell commands in the background under the same task space — `execute` gains a `run_in_background` argument, so a long command (e.g. a test suite) returns a task id immediately while the agent keeps working, polls interim output with `task_output`, and cancels with `task_stop`. Foreground shell behavior is unchanged: no timer, no auto-backgrounding — backgrounding is explicit only. Same process-local honesty: a background command is a child of the server process and does not survive a restart.
- Delegation execution is isolated: sub-agent execution events do not persist onto or stream onto the parent transcript; the delegation appears as exactly one tool-call card, live and on reload.

## Capabilities

### New Capabilities
- `agent-subagents`: agent delegation — the delegation tool, the injected general-purpose subagent, declarable subagent types, background delegation with `task_output`/`task_stop`, child-transcript isolation, and capability enablement rules.
- `agent-background-shell`: background execution for the agent's shell tool under the same run-scoped task space — the `run_in_background` execution mode, unchanged foreground semantics, and the shared control-tool and completion-notification behavior.

### Modified Capabilities
- `agent-composition`: the fixed middleware order gains the subagent and background-control steps; composition gains the subagent wiring inputs (subagent instances, background config, general-subagent suppression) under the existing purity contract.

## Impact

- `go.mod` / `go.sum` — eino alpha.35 (eino-ext modules resolve via MVS; build and tests gate).
- `internal/agents/agent.go` (Config + buildMiddlewares + clone builder + fs background wiring), `internal/agents/runner.go` (capability resolution from the allowlist, per-run background runner), `internal/agents/stream.go` / translator (child-event filtering), `internal/agents/tool_catalog.go` (catalog rows + reserved names), `internal/agents/backend` (append-opener adapter for output files), `internal/agents/context_breakdown.go` (accounting for the middleware-injected instruction).
- Session events: a new completion transcript event class for background-task completion.
- No database migration, no REST API change; the delegation tool is model-facing. Web needs no changes (the delegation card renders through the existing tool-call card).
- `scripts/smoke.sh` gains a delegation scenario against the stub LLM.
