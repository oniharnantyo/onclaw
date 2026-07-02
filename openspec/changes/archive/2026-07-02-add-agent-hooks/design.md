## Context

onclaw runs a single eino `TypedChatModelAgent` per turn (`internal/agent/agent.go`). eino already provides the interception mechanism we need — `adk.TypedChatModelAgentMiddleware`, used today by the `history`, `summarization`, and `skill` middlewares (registered in the `Handlers` slice at `agent.go:188`). What is missing is a **user-facing hook system** on top of that mechanism: a way for users to attach their own logic (shell commands or inline scripts) to lifecycle events with allow/block semantics, stored in the DB and managed through the same CLI/API/Web-UI pattern onclaw uses for skills and MCP.

This change ports GoClaw's hook *model* onto onclaw's *substrate*, scoped to a deliberate v1. onclaw's hard constraints shape every decision: single-user (no tenants), pure-Go + `CGO_ENABLED=0` (static binary, ARM cross-compile), and low-resource (~2 GB RAM).

## Goals / Non-Goals

**Goals:**
- Let users intercept/observe the agent loop at five lifecycle events with allow/block semantics (fail-closed).
- Support two v1 handler types: `command` (shell) and `script` (inline JS via an embedded pure-Go runtime).
- Make hooks DB-backed, auditable, and manageable via CLI + REST API + Web UI — mirroring skills/MCP.
- Make session lifecycle channel-agnostic so the Web UI and future channels fire events identically to the CLI.
- Add zero behavioral change when no hooks are configured.

**Non-Goals (deferred to v2):**
- `http` (webhook) and `prompt` (LLM-evaluator) handler types.
- `subagent_start`/`subagent_stop` events (onclaw has no subagents today).
- CEL `if_expr` matchers (v1 ships regex on `tool_name` only).
- Multi-tenant scoping, edition gating, token budgets, and SSRF protection (single-user; belong to the deferred `http` handler).

## Decisions

### Decision: Emit events through eino middleware, not custom instrumentation
The agent-loop events (`user_prompt_submit`, `pre_tool_use`, `post_tool_use`, `stop`) map directly onto existing eino middleware hooks (`BeforeAgent`, `Wrap*ToolCall`, `AfterAgent`). Reusing them gives us correct ordering, state access, and streaming integration for free, and matches the established `history_middleware.go` pattern.
- **Alternatives considered:** (a) a bespoke event bus instrumenting every call site — more code, drift-prone, duplicates what eino already does; (b) eino `callbacks.Handler` — read-only by contract, cannot implement blocking/allow semantics. Middleware is the only eino mechanism that can *control* behavior.

### Decision: A standalone `internal/hooks` dispatcher with a thin eino bridge
All hook logic (resolution, matching, execution, safeguards, audit) lives in `internal/hooks`. The eino-specific glue is a thin `hooks_middleware.go` in `internal/agent/middlewares` that calls the dispatcher. This keeps the dispatcher testable in isolation and reusable by non-eino entry points (the CLI/API fire `session_start`/error-`stop` directly through it).
- **Alternative:** putting everything inside the middleware couples hook logic to eino types and makes the CLI emitters awkward.

### Decision: Channel-agnostic session lifecycle via `Agent.StartSession`/`EndSession`
Session lifecycle is integrated INTO the agent — `Run` fires `session_start` once via `sync.Once` on the first turn; the `eventIterator` fires `stop` when a turn terminates with an error; `AfterAgent` fires normal `stop`; the channel is supplied at `AssembleAgent` (threaded via `agentSessionRequest.Channel`) and carried in event payloads. Channels only call `Run` + iterate — no `StartSession`/`EndSession` methods exist on the public contract. Rationale: turns a per-channel discipline into a guarantee enforced by `Run` being the single entry point. Note the `sync.Once` makes "session vs turn" correct because the agent lives for the whole session while `Run` is per-turn.

### Decision: `script` handler uses goja (pure-Go JS), chosen over the alternatives
onclaw's `CGO_ENABLED=0` + ARM cross-compile constraints eliminate anything with a C toolchain. Within that envelope:
- **goja** (pure-Go, no CGO, ~2–3 MB, in-process, sandboxable by default) — chosen.
- **v8go / QuickJS bindings** — require CGO; break the static binary and cross-compile. Rejected.
- **External Node via the `command` handler** — already possible (`command: "node hook.js"`) but depends on Node being installed on the device, which is not guaranteed on a Pi. Kept as the zero-code fallback.
- **Lua (gopher-lua) / Starlark / Tengo** — lighter or more deterministic, but JS is more universally known and goja's sandbox + `console` binding fit the decide-then-exit hook shape. Lua remains a viable future alternative if footprint becomes a problem.

### Decision: Handler types behind a registry-backed interface

`command` and `script` are not special-cased in the dispatcher; each is a `Handler` implementation registered by type name. `internal/hooks/handler.go` defines:

```go
type Handler interface {
    Run(ctx context.Context, payload Payload) (Decision, error)
}
type HandlerFactory func(cfg []byte) (Handler, error)
func Register(handlerType string, f HandlerFactory)
func New(handlerType string, cfg []byte) (Handler, error) // lookup + build
```

The dispatcher resolves a hook's handler via `hooks.New(hook.HandlerType, hook.Config)` (cached per hook id+version) and calls `Run`. `command.go` and `script.go` each ship a factory (`commandFactory`, `scriptFactory`) registered explicitly during assembly. Adding `http`/`prompt` later is one file plus one `Register` call — the dispatcher never changes.
- **Why:** matches onclaw's existing registry idiom (`internal/agent/tools/registry.go`, `llm.adapter.Registry`), keeps the dispatcher free of type switches, lets the goja program be parsed once at factory time and reused per `Run` with a fresh sandboxed runtime, and makes each handler unit-testable in isolation.
- **Alternative considered:** a `switch hook.HandlerType` in the dispatcher — functional for v1, but couples the dispatcher to every handler type and makes each extension invasive.

### Decision: Block = deny-but-continue via a wrapped tool endpoint, not an error
For `pre_tool_use`, a `block` returns a wrapped `InvokableToolCallEndpoint`/`StreamableToolCallEndpoint` that yields a *denied tool result* (not a Go error). The model sees the denial reason as the tool's output and can adapt; the run continues. Returning a raw error would abort the whole turn (eino's fail-fast contract) — undesirable for a per-tool guard.
- **Note:** the implementer must verify the exact endpoint type signatures in `adk/handler.go` (`eino@v0.10.0-alpha.9`) before coding the wrap logic.

### Decision: Scopes are `global` + `agent` only
onclaw is single-user, so GoClaw's `tenant` scope is meaningless here. Two scopes match the existing skill/MCP model and the `store.Agent` concept; resolution is global → agent, priority-descending, block short-circuits.

### Decision: Safeguards are in-process and per-session
Per-hook timeout, chain budget, circuit breaker, and fail-closed all live in the dispatcher. The circuit breaker is in-memory per process/session (resets on restart) — acceptable for v1. The `command` handler strips the environment to an allowlist to prevent leaking configured secrets into user scripts.

### Decision: Mirror the skill/MCP storage and management pattern exactly
`HookStore`/`HookExecutionStore` interfaces in `internal/store/store.go`, DTOs in `types.go`, SQLite impl in `internal/store/sqlite/hooks.go`, migration in `db.go`, CLI in `cli/hooks_cmd.go`, REST in `api/handler`+`api/service`, Web panel in `web/src/components/Hooks.tsx`. This is the third instance of an established pattern — minimal novelty, high reuse.

## Risks / Trade-offs

- **[Risk] eino `Wrap*ToolCall` deny-but-continue semantics are subtle** → Mitigation: read the live `adk/handler.go` signatures before implementing; add a unit test asserting a blocked tool yields a denied *result* (not an error) and the run continues.
- **[Risk] goja has no native context cancellation** → Mitigation: enforce the per-hook timeout by running the script with a timer goroutine that calls `vm.Interrupt()`; treat the resulting interrupt as an error subject to `on_timeout`.
- **[Trade-off] goja lacks `async`/`await` and native ES modules** → Acceptable: hook scripts are small, synchronous, decide-then-exit. Document the limitation.
- **[Trade-off] Circuit-breaker state is in-memory and resets on restart** → Accepted for v1; persisted `enabled=false` (set when the breaker trips) survives restarts, so a tripped hook stays disabled.
- **[Risk] Script sandbox escape if richer APIs are bound later** → Mitigation: v1 binds only the read-only event `ctx` and a captured `console`; `require`/fs/network stay disabled.

## Migration Plan

- **Forward:** `db.go` `Migrate()` adds `agent_hooks` and `hook_executions` via idempotent `CREATE TABLE IF NOT EXISTS`. No data migration is needed; the feature is opt-in and a no-op when no hooks exist. The hooks middleware is always registered, but with an empty hook set it fires no handlers.
- **Rollback:** remove the middleware from the `Handlers` slice and drop the two tables. No existing behavior depends on the new tables.

## Open Questions

- When to add CEL `if_expr` matchers (v2) — needed for `channel`-aware filtering, since v1's regex matcher is `tool_name`-only.
- Whether the future `prompt` (LLM-evaluator) handler should reuse `llm.Service.Build` and how to bound its cost on a low-resource device.
- Whether `subagent_*` events should be stubbed now (no-op emitters) so future subagent work is wiring-only, or added only when subagents exist.