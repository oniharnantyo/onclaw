## Why

onclaw has no way for users to intercept, observe, or guard the agent loop — block an unsafe tool call, auto-audit after a write, inject session context, or notify on stop. eino exposes low-level middleware hook points (already used internally by `history`/`summarization`/`skill`), but there is no user-facing hook system on top of them. This adds a configurable, DB-backed hook system (modeled on GoClaw) so users can attach `command` (shell) or `script` (inline JS) handlers to lifecycle events with allow/block semantics, managed via CLI, REST API, and Web UI.

## What Changes

- Introduce a **lifecycle hook system** with five events: `session_start`, `user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `stop`. `user_prompt_submit` and `pre_tool_use` are **blocking** (return an allow/block decision); the rest are non-blocking (observation only).
- Two handler types in v1: **`command`** (shell command; stdin = JSON event payload; exit 0 = allow, exit 2 = block with stderr fed back to the model, other non-zero = error) and **`script`** (inline JavaScript executed in-process by an embedded **goja** runtime, pure-Go and sandboxed). `http` and `prompt` (LLM-evaluator) handlers are deferred to v2.
- **Scopes**: `global` and `agent` (onclaw is single-user; GoClaw's `tenant` scope is dropped). Hooks resolve global → agent by priority; one `block` short-circuits the chain.
- **Matchers**: regex on `tool_name` (Go stdlib) in v1; CEL `if_expr` deferred to v2.
- A **dispatcher** with safeguards: per-hook timeout (5s default, 10s max), chain budget, circuit breaker (auto-disables a hook after 5 blocks/timeouts in 1 minute), env allowlist (command handler), and **fail-closed** behavior for blocking events.
- **DB-backed storage + audit**: `agent_hooks` (definitions) and `hook_executions` (audit log) tables.
- **Channel-agnostic session lifecycle**: every entry point (CLI `run`/`chat`, API/Web UI via `serve`, and future channels) fires `session_start`/error-`stop` through a shared `Agent.StartSession`/`EndSession` contract.
- **Management surface**: `onclaw hooks add/list/show/remove/toggle/test` CLI, REST endpoints under `/api/hooks`, and a Web UI Hooks panel — mirroring the existing skill/MCP feature pattern.
- **No breaking changes**: with no hooks configured, the agent behaves exactly as today (hooks are opt-in).

## Capabilities

### New Capabilities

- `agent-hooks`: lifecycle hook system — the five events, blocking/non-blocking semantics, the `command` and `script` handler types, the dispatcher (resolution, matching, execution, safeguards), allow/block decisions (fail-closed), audit logging, DB storage, and the full management surface (CLI + REST API + Web UI). This capability also defines the contract by which the agent run loop emits events and becomes interceptable.

### Modified Capabilities

<!-- None at the spec-requirement level. The change integrates with the agent run loop (internal/agent) and the web UI (new Hooks panel), but the new behavior is specified within `agent-hooks`; existing `agent-core` and `web-ui` requirements are not altered. -->

## Impact

- **New packages/files**: `internal/hooks` (types, dispatcher, command handler, goja script handler, regex matcher); `internal/agent/middlewares/hooks_middleware.go` (eino bridge); `internal/store/sqlite/hooks.go` (store impl) + `HookStore`/`HookExecutionStore` interfaces in `internal/store/store.go` and `Hook`/`HookExecution` DTOs in `internal/store/types.go`; `internal/cli/hooks_cmd.go`; `internal/api/handler/hooks.go` + `internal/api/service/hooks.go`; `web/src/components/Hooks.tsx`.
- **Modified files**: `internal/agent/agent.go` (`AssembleAgent` wires the hooks middleware into the `Handlers` slice and exposes `StartSession`/`EndSession`); `internal/cli/run.go`, `internal/cli/chat.go`, and the API `serve` handlers (fire session lifecycle); `internal/store/sqlite/db.go` (migration); `internal/cli/app.go` and `internal/api/routes.go` (register CLI command + REST routes); `web/src/App.tsx` (wire panel).
- **New dependency**: `github.com/dop251/goja` (pure-Go JavaScript, no CGO — honors `CGO_ENABLED=0` and ARM cross-compile).
- **Database migration**: adds `agent_hooks` and `hook_executions` tables.
- **Deferred to v2**: `http` and `prompt` handler types, `subagent_start`/`subagent_stop` events (onclaw has no subagents today), CEL `if_expr` matchers.