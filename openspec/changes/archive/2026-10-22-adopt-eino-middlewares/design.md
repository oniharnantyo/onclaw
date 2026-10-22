# Design

## Context

The runtime already composes eino middlewares in one ordered list (`buildHandlers` in compose.go: patchtoolcalls → skill → filesystem → hooks → tool-error-result); the list is the documented extension seam. Five of the middlewares named in the exploration are not yet adopted. The governing architectural fact: buddies is **stateless per turn** — every turn rehydrates model input from the sqlite event log (user/assistant text after the checkpoint boundary), runs one ephemeral ADK run, and drains events back into the log. Cross-turn memory is the persisted checkpoint; ADK state dies with the turn. All five middlewares operate on in-run state, so each lands on a different layer of that architecture. `tools/`, `mcp/`, `hooks/`, `backend/` already sit as subpackages; the root still carries ~40 files sharing one package.

## Goals / Non-Goals

Goals:
- Replace hand-rolled capability code with the pinned eino version's middlewares where the behavioral contract can be preserved through adapters.
- Give single turns the same context protection cross-turn compaction already gives sessions.
- Split the `internal/agents` root into concern subpackages on the settled tree.

Non-Goals:
- Replacing checkpoint compaction with in-run summarization (the checkpoint stays the persisted cross-turn mechanism).
- Migrating old `.todo.json` plans.
- Adopting `agentsmd`, `automemory`, or `dynamictool` (not in scope; no consuming feature exists).
- Any frontend behavior change beyond regenerated binding paths.

## Decisions

### D1: Two-layer context management (summarization does not replace compaction)

Checkpoint compaction (`compact.go`, pre-turn over the DB log) stays the source of truth for cross-turn context. The eino summarization middleware joins as an **in-run safety net**: trigger at ~90% of the model's context limit (above the 0.8 compaction threshold, so the checkpoint layer fires first between turns), `TokenCounter` wired to the same estimator (`charsPerToken` × `m.contextLimit`), default `Finalize` (in-run history replacement only). The in-run summary is ephemeral by design: it dies with the turn, is never persisted as a checkpoint, and never advances `UpToSeq`. Alternative considered — syncing the middleware's summary into `sessions.Checkpoint` via its `Callback` — rejected: in-run messages are not 1:1 with DB events, so the boundary arithmetic would be invented complexity for no user-visible gain.

### D2: Reduction before summarization, jail as the offload Backend

Handler order places reduction before summarization so oversized payloads are offloaded **before** the summarizer reads them (the summary sees notices, not a 50 KB dump). The reduction `Backend` is the agent's jailed workspace via the existing `backend` package — offloaded files land inside the jail and are readable back through the fs middleware's `read_file` (reduction's `ReadFileToolName` matches). Both phases enabled: truncation with a fixed length bound (the middleware default scale), clear phase with `MaxTokensForClear` derived from `m.contextLimit` and a retained-round suffix. Agents whose fs tools are fully disabled still get reduction (offload succeeds; the notice just names a file the model cannot open) — acceptable degradation, never a failure (spec: offload failure keeps the original).

### D3: plantask replaces the todo pair; gating follows the fs pattern

`plantask.New` with `BaseDir` inside the jailed workspace replaces `tools/todo.go` (deleted). Its `Backend` is the same `filesystem` interface family the jail already implements — the adapter is thin. The four `Task*` tools are middleware-registered like fs tools and gate by name through the turn's resolved surface (the `fsDisabled` mechanism generalized): catalog rows for the four names replace the two todo rows in the Tools pane and allowlist chips. `Task*` store numbered JSON files plus a `.highwatermark` in BaseDir; they are workspace documents like any other. Old `.todo.json` is dropped (user decision): plans are ephemeral agent state.

### D4: subagent + backgroundtask swap preserves the contract through adapters

One eino `subagent.New` per turn (composition is already per-turn) with `SubAgents` = the single parent-clone agent composed from the parent's finished data (instruction, model, resolved surface, skills, hooks — exactly today's clone recipe, no recursion because the clone's surface never contains delegation tools). `Background.Local` carries the `backgroundlocal.Runner`, `OutputDir` = the workspace `.tasks/` directory, and a `TranscriptFormat` that reproduces today's transcript entry shape. A `backgroundtask.New` middleware bound to the same Manager injects `task_output`/`task_stop` (replacing `subagent_tools.go`). The parity seams that survive as adapters:
- **task_activity records**: the run-scoped `taskRegistry` stays as the bridge — it subscribes to Manager lifecycle and emits `sessions.TaskActivity` events onto the parent transcript (UI contract, spec agent-subagents req 5).
- **Session-scoped task IDs**: one Manager instance per run gives the same per-run ID space.
- **Shell approvals**: children compose with the parent's `store.CheckpointStore` as today.
- **Hooks**: the hooks middleware rides the child composition as today; hooks matching `agent`/`task_output`/`task_stop` by name keep working — the middleware's tool names are unchanged.

#### Parity Checklist (taskRegistry -> Eino Manager)

| Feature / Behavior | Today's taskRegistry | Eino Manager Equivalent |
|---|---|---|
| 1. Task ID Generation | Sequential "task-1", "task-2"... | bgtask.Config.IDGen custom closure |
| 2. Spawning / Registration | registry.spawn(desc, bg, turnID) | runner.Run -> mgr.CreateTask |
| 3. State: Running | taskStatusRunning ("running") | bgtask.StatusRunning |
| 4. State: Completed | taskStatusCompleted ("completed") | bgtask.StatusCompleted |
| 5. State: Canceled / Stopped | taskStatusStopped ("stopped") | bgtask.StatusCanceled |
| 6. State: Failed | taskStatusFailed ("failed") | bgtask.StatusFailed |
| 7. Output directory | .tasks/ in workspace | LocalBackgroundConfig.OutputDir |
| 8. Transcript format | JSONL transcriptEntry steps | subagent.TranscriptFormat closure |
| 9. Control: task_output | subagent_tools.go task_output | bgmiddleware.New -> task_output |
| 10. Control: task_stop | subagent_tools.go task_stop | bgmiddleware.New -> task_stop |
| 11. Parent event bridge | registry.emitTask -> session ev | taskRegistry as Manager bridge |
| 12. No recursion | Clone surface omits agent tool | SubAgents clone omits agent tool |

### D5: Package split on the settled tree

After the swaps delete `todo.go` and most of `subagent*.go`, split the root:

```
internal/agents/
  (root)      manager, manager_impl, registry, catalog, builtin_tools, connectors,
              prompts, regenerate, verify, types, doc — the service surface
  compose/    compose.go, gate.go, resolver.go, context.go, handler wiring
  run/        runner.go, turn.go, turninput.go, resume.go, subscription.go, drain
  compaction/ compact.go (+ the summarization safety-net config)
  subagents/  the shrunken delegation adapter
  tools/      unchanged (browser, webfetch, websearch — user decision: stays)
  mcp/ hooks/ backend/  unchanged
```

Rule applied: a package moves out of `internal/agents` only if it has no agent-concept dependencies; only `tools/` qualified and the user explicitly kept it inside. Cross-package seams (e.g. `toolErrorResult`, `resolvedTools`, the drain function values) get exported as needed — churn accepted. Bound types that move (`ContextUsage` et al.) regenerate under new binding paths; frontend imports follow mechanically.

### D6: Tool-error-result stays hand-rolled; hooks stays hand-rolled

The eino tag has no equivalent for either. The tool-error-result middleware must keep wrapping middleware-registered tools (reduction offload files, Task\* tools) exactly as it wraps fs/skill tools today — appended last, order unchanged.

## Risks / Trade-offs

- [Manager lifecycle differs from taskRegistry's contract] → Spike first (W2 task 1): run one foreground + one background delegation through the middleware against a fake model before deleting any hand-rolled code; keep `subagent*.go` in the tree until parity is demonstrated.
- [Two summarizers could both fire on one turn] → Layered thresholds (0.8 checkpoint pre-turn, ~0.9 in-run) and distinct scopes (DB log vs in-run state); tests pin that a checkpoint compaction is never triggered by the in-run path.
- [plantask files visible in workspace listings] → Numbered task files are legitimate workspace documents the agent may read; `.highwatermark` is a dotfile. Accepted.
- [Bindings churn breaks frontend builds mid-wave] → Regen bindings at wave end only (`task common:generate:bindings --force`), never mid-wave (documented gotcha: breaks parallel frontend work).
- [Hooks users match `todo_write` by name] → Breaking rename documented in the proposal; no silent alias (an alias would contradict the honest-name gate convention).

## Migration Plan

Waves: W1 reduction + plantask (standalone, low risk) → W2 subagent + backgroundtask (spike, swap, delete) → W3 summarization safety net → W4 package split + bindings regen + frontend import updates → W5 spec sync + verify. Rollback is per-wave: each wave leaves `go test ./...` green and the app functional.

## Open Questions

- Exact transcript entry shape emitted by `TranscriptFormat` vs today's `transcriptEntry` — resolved during W2 against the existing `.tasks/` files' consumer (the fs tools); no spec impact.
- Whether the summarization middleware's `EmitInternalEvents` should surface in-run compactions in the UI (today's design: no — the transcript is untouched; revisit only if the context ring misleads during safety-net turns).
