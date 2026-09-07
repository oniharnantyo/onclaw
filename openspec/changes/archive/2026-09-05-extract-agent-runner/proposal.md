# Proposal: extract-agent-runner

## Why

The previous consolidation left `internal/agents/agent.go` as two files glued together: the pure composition step (lines 1–224 — `Config`, `validateConfig`, `buildMiddlewares`, `Compose`, `skillBackend`, `offloadTranscript`) followed by the tenant-aware execution engine (lines 226–743 — the `Agent` struct holding eight granular stores, the `AgentOption` knobs, `NewAgent`, and the pipeline methods `load` → `resolve` → `composeAgent` → `execute` → `Run`/`streamRun`). The two halves have nothing in common: composition is a pure, stateless build step (capability `agent-composition`), while the engine resolves tenant state and drives execution (capability `agent-runtime`). Keeping them in one file erases the seam the composition capability was carved to make. The user directive: **`agent.go` keeps only `Compose`**; the engine becomes a runner.

Two naming problems come with the move. The runtime type is `agents.Agent` while the stored entity is `domain.Agent` — same name, different packages, constant reader friction — and the Eino guidance is explicit that a *Runner* executes composed agents ("use `Runner` to execute agents — never call `agent.Run()` directly in production"). Renaming the runtime type to `Runner` fixes both at once.

While moving the engine, three nil guards flagged in the `consolidate-agent-engine-history` verification report must go: `internal/server/handlers/agents.go:568` (`if h.engine == nil` → dead empty-result branch), `internal/agents/history.go:36` (`e.agents != nil`), and `internal/agents/history.go:42` (`e.sessionEvents == nil` → silent empty result). AGENTS.md forbids nil guards on injected dependencies — the composition root (the router's fallback construction, `router.go:91–105`) already guarantees they are non-nil.

## What Changes

- **Engine moves verbatim to `internal/agents/runner.go`.** The `Agent` struct, `AgentOption`s, `NewAgent`, `resolvedContextWindow`, `load`, `resolve`, `composeAgent`, `execute`, `Run`, and `streamRun` leave `agent.go` unchanged in behavior; `agent.go` retains only the composition step. The section banners in `agent.go` are dropped and its imports pruned.
- **Runtime type renamed `Agent` → `Runner`.** `AgentOption` → `RunnerOption`, `NewAgent` → `NewRunner`, receiver `e` → `r`; the `With*` option names are unchanged. `RouterOptions.Engine` becomes `RouterOptions.Runner`; the construction sites in `internal/cli/server.go` and the router fallback call `agents.NewRunner`. This removes the `agents.Agent` (runtime) vs `domain.Agent` (entity) collision and matches the Eino ADK vocabulary — the runner wraps the agent produced by `Compose()` in `adk.NewTypedRunner` and drives its typed event iterator.
- **`History` stays a Runner method.** It already shares the runner's `sessionEvents` and `agents` stores; the HTTP layer depends only on the narrow `AgentHistoryReader` interface, so handlers are unaffected beyond the receiver rename.
- **Nil guards removed.** The three guards listed above are deleted; a missing dependency now fails loudly at construction/use instead of silently returning empty history. The router's composition-root fallback (the sanctioned default-resolution pattern) is the only place a nil `RouterOptions.Runner` is interpreted.
- **Tests follow the code.** Engine tests (`TestNewAgent_DefaultsAndOptions`, `TestAgent_Run_Validation`, `TestResolvedContextWindow`) move to a new `runner_test.go` renamed `TestNewRunner_*` / `TestRunner_*`; `history_test.go` and `smoke_history_test.go` switch to `NewRunner` / `RouterOptions{Runner: …}`. Composition tests stay in `agent_test.go`.
- **Package cleanup.** After the split, a dead-code/overengineering audit of `internal/agents/` removes the remaining scaffolding: the `middlewares/` subpackage (six pass-through constructors whose only consumer is `buildMiddlewares` — inlined, subpackage deleted, no import cycle was ever real); dead symbols (`ToolFilter.All()` with zero callers, write-only `agentConfig.Tools`, never-called `IsTerminal()`, never-emitted `tool_call_requested`/`retrying` constants — `reasoning_delta` stays, it is spec-mandated vocabulary); `config.go` dissolves (runner-pipeline types → `runner.go`, shared capability structs + `DefaultMaxIterations` → `agent.go`); `instruction_composer.go` merges into `runner.go` (single caller, spec-fixed document order). `tool_registry.go`, `model_factory.go`, `session_adapter.go`, `jail.go`, `skills_resolver.go` stay separate deliberately — extension seams (tools, provider types), a cross-package port adapter (session persistence, consumed by the Postgres conformance test), and heavy implementations with dedicated test suites. Final layout: 10 source files, `agent.go` ≈ 300 lines (composition), `runner.go` ≈ 680 (pipeline).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

(none — behavior-preserving refactor. Both affected specs are behavior-only: `agent-runtime`'s requirements (streaming execution, session history, cancellation, bounded loop, …) and `agent-composition`'s requirements (pure composition, fail-fast validation, middleware order, …) reference no type or file names, and none of them change.)

## Impact

- **Backend:** `internal/agents/agent.go` (truncated to composition), `internal/agents/runner.go` (new — the engine), `internal/agents/history.go` (receiver rename, guard removal), `internal/agents/doc.go` (key type `[Runner]`, architecture wording), `internal/server/router.go` (`RouterOptions.Runner` + fallback), `internal/server/handlers/agents.go` (field rename, nil guard removed), `internal/cli/server.go` (`agents.NewRunner`).
- **Tests:** `internal/agents/agent_test.go` (composition tests stay), `internal/agents/runner_test.go` (new — moved engine tests), `internal/agents/history_test.go`, `internal/server/smoke_history_test.go`. `router_test.go` needs no change (it exercises the fallback path).
- **No migrations, no API changes, no frontend changes.** The `GET /workspaces/:ws/agents/:agent/sessions/:session/events` endpoint, the `EventStream`/`TranscriptEvent` contracts, and all execution behavior are untouched.
