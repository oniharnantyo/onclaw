# Proposal

## Why

`internal/agents` hand-rolls capabilities that eino `v0.10.0-alpha.35` (already the pinned version) ships as ADK middlewares: the delegation stack (~1,000 lines across subagent.go, subagent_tasks.go, subagent_tools.go, subagent_transcript.go) and the todo plan pair, while oversized tool outputs are never truncated and a single long turn can balloon past the context window. Adopting the upstream middlewares deletes hand-rolled machinery, gains battle-tested context management, and — in the same pass — settles the 40-file root package into navigable subpackages.

## What Changes

- Adopt the eino `reduction` middleware: tool outputs exceeding a length bound are offloaded into the agent's jailed workspace and the in-transcript result becomes a truncated notice pointing at the offloaded file (readable back through `read_file`). Configured with the model's context limit and the same token estimator the runtime already uses.
- Adopt the eino `summarization` middleware as an **in-run safety net**: when a single turn's working state crosses a higher threshold of the context limit mid-iterations, the middleware collapses the turn's message state. Checkpoint compaction (spec agent-runtime "Compaction") stays the persisted cross-turn mechanism and is untouched.
- **BREAKING** Adopt the eino `plantask` middleware: the `todo_write`/`todo_read` pair is replaced by `task_create`/`task_get`/`task_update`/`task_list` backed by the same jailed workspace. Existing `.todo.json` plans are dropped, not migrated (plans are ephemeral agent state).
- Adopt the eino `subagent` + `backgroundtask` middlewares: the hand-rolled delegation stack is replaced by the middleware's `agent` tool and `task_output`/`task_stop` control tools over a shared background-task Manager. The behavioral contract is preserved: `task_activity` records on the parent transcript, no-recursion, the `subagents_enabled` gate, run-scoped task IDs, `.tasks/` transcript materialization, and children sharing the parent's shell-approval checkpoints.
- Compose the adopted middlewares into the fixed handler order (patchtoolcalls → reduction → summarization → skill → filesystem → hooks → subagent → backgroundtask → tool-error-result); middleware-registered tools (Task\*) gate by name through the turn's tool gate like the fs tools already do.
- Split the `internal/agents` root into subpackages on the settled tree: `compose/` (composition + middleware wiring), `run/` (turn loop, drain, resume), `compaction/` (checkpoint layer), `subagents/` (the shrunken delegation adapter). `tools/`, `mcp/`, `hooks/`, `backend/` stay put.
- Regenerate Wails bindings after the split; bound types that move packages change generated paths and frontend imports follow mechanically.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: new requirements for the in-run context safety net (summarization) and tool-output reduction, composing with — not replacing — checkpoint compaction.
- `agent-tools`: the todo requirement becomes the plantask tool set (task_create/task_get/task_update/task_list), still workspace-persisted and still gated through the turn's tool gate.

## Impact

- `internal/agents/**` — the bulk of the work: middleware adapters, delegation stack replacement, package split. `internal/agents/tools/todo.go` deleted; `subagent*.go` largely deleted.
- `frontend/bindings/**` and frontend imports — regenerated paths for moved bound types.
- Specs touched: `agent-runtime`, `agent-tools`. `agent-subagents` requirements are behaviorally preserved by the swap and take no delta.
- Hooks that match `todo_write`/`todo_read` by name stop matching (tool names changed); user-facing release note.
- Dependencies: no new modules — all five middlewares ship in the pinned eino version.
