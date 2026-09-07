# Design: extract-agent-runner

## Context

`internal/agents/agent.go` is 743 lines with two section banners:

```
internal/agents/agent.go
  L28–224   Composition — Config, validateConfig, buildMiddlewares, Compose,
            skillBackend, offloadTranscript   (pure, stateless; capability agent-composition)
  L226–743  Engine — Agent struct (8 stores + key + dir + 5 knobs), AgentOptions,
            NewAgent, resolvedContextWindow, load, resolve, composeAgent, execute,
            Run, streamRun                    (tenant state; capability agent-runtime)
```

`history.go` adds `(e *Agent) History` and is coupled to the engine half (`e.agents`, `e.sessionEvents`).

External engine surface (everything the split and rename touch):

- `internal/cli/server.go:137` — `agents.NewAgent(8 stores, encKey, onClawDir)`, passed as `RouterOptions.Engine` (:162).
- `internal/server/router.go:26` — `RouterOptions.Engine *agents.Agent`; :90–105 the fallback constructs a default engine when `RouterOptions` supplies none (the sanctioned composition-root default); :108 passes it into `NewAgentHandlers`.
- `internal/server/handlers/agents.go:25` — handlers depend only on the narrow `AgentHistoryReader` interface (`History`); field `engine`, call at :571. The five handler tests that pass a nil engine never reach `ListSessionEvents` (verified by grep).
- Tests: `agent_test.go` engine tests at :377–446, `history_test.go` `setupHistoryTest`, `smoke_history_test.go` (full Run → history pipeline, `RouterOptions{Engine: engine}`); `router_test.go` exercises the fallback path and needs no change.

Eino guidance (eino-agent skill): "Use `Runner` to execute agents — never call `agent.Run()` directly in production." The engine half is exactly that production Runner: it wraps the `adk.TypedResumableAgent` produced by `Compose()` in `adk.NewTypedRunner(TypedRunnerConfig{Agent, EnableStreaming: true, CheckPointStore, SessionID, SessionStore})` and drives the typed event iterator onto domain `TranscriptEvent`s.

## Goals / Non-Goals

- Goals: `agent.go` holds only the composition step; the engine lives in `runner.go` under a name that says what it does; the verification report's nil-guard warnings are closed.
- Non-Goals: no behavior change of any kind (execution, `EventStream`/`TranscriptEvent` contracts, endpoints, storage); no cancellation redesign — ADK's safe-point cancel modes (`adk.WithCancel()` with `CancelAfterChatModel`/`CancelAfterToolCalls`) are a candidate future hardening, but the agent-runtime spec's "Cancellation at a safe point" is already met by the current context-cancel + session cancel markers; no History extraction into a separate type; no API, schema, or frontend changes.

## Decisions

### D1: Verbatim split — engine to `runner.go`, `agent.go` keeps only composition

The engine half moves to `runner.go` byte-for-byte (no logic edits), so the diff proves behavior preservation. `agent.go` keeps lines 1–224: `Config`, `validateConfig`, `buildMiddlewares`, `Compose`, `skillBackend`, `offloadTranscript`; the two section banners are dropped and the now-unused imports (`io`, `log/slog`, `time`, `providers`, `secrets`, `store`, `domain`) are pruned. `doc.go` is updated: key type `[Runner]`, architecture wording — agent.go composes (pure build step), runner.go loads/resolves/composes-via-`Compose`/executes.

### D2: Rename the runtime type `Agent` → `Runner`

`AgentOption` → `RunnerOption`, `NewAgent` → `NewRunner`, receiver `e` → `r`; `With*` option names stay (they name knobs, not the type). Rationale: (a) the user's own vocabulary — "explore how the runner will be implemented"; (b) Eino ADK alignment — a Runner wraps a composed agent and manages execution; (c) it removes the standing `agents.Agent` (runtime) vs `domain.Agent` (stored entity) collision — the runner executes domain agents. `RouterOptions.Engine` becomes `RouterOptions.Runner` so the composition root's language matches.

### D3: Runner shape — 8 granular stores + key + dir, knobs as functional options, Run pipeline unchanged

Per AGENTS.md DI rules, `NewRunner` keeps the positional granular-store constructor (`workspaces, agents, users, members, roles, providers, sessionEvents, checkpoints, encryptionKey, onClawDir, opts ...RunnerOption`) and the defaultable knobs stay functional options (`WithAgenticModelFactory`, `WithInstructionComposer`, `WithToolRegistry`, `WithSummarizationMargin`, `WithMaxIterations`). `Run(ctx, ExecRequest)` keeps the four-phase pipeline: validate → **Load** (workspace/agent/user/membership/role, workspace-scoped) → **Resolve** (denylist tools, provider credential decrypt, model factory, context window → summarization trigger, capability configs) → **Compose** (instruction composition, then delegate to the pure `Compose()` in agent.go) → **Execute** (`adk.NewTypedRunner`, goroutine mapping ADK events onto the `EventStream`).

`History` stays a method on `Runner`: it uses the runner's `sessionEvents` and `agents` stores, and the HTTP layer already depends on the narrow `AgentHistoryReader` interface, so no handler construction changes beyond the receiver rename. A separate HistoryReader type would duplicate construction for no seam value today.

### D4: Remove the three nil guards — composition root guarantees non-nil

The guards are on injected dependencies, which AGENTS.md forbids (`if x != nil` defensive guards; nil checks are reserved for optional payloads, optional response data, and errors):

- `handlers/agents.go:568` `if h.engine == nil` → dead branch returning an empty result; the router always passes a non-nil runner (explicit or fallback), so delete it.
- `history.go:36` `e.agents != nil` → the store is constructor-injected and non-nil by the DI rule; delete the guard, keep the `req.AgentID != ""` check (that one tests an optional request field, which is allowed).
- `history.go:42` `e.sessionEvents == nil` → same rule; delete. A missing store now panics at first use instead of silently returning "no history" — the failure mode AGENTS.md intends.

The router's `if engine == nil && rt.opts.Store != nil` fallback stays: it interprets an optional caller-supplied `RouterOptions` payload and supplies the built-in default, which is the sanctioned composition-root pattern, not a defensive guard.

### D5: Package cleanup — merge the shims, keep the seams

Audit criterion (user-supplied): a file merges when it has a single caller and no extension axis; it stays when it is a deliberate seam or too heavy to inline.

- **`middlewares/` subpackage → deleted, bodies inlined into `buildMiddlewares`.** Every constructor was a one-statement config-fill plus error wrap, consumed only by `buildMiddlewares`, which re-wraps errors anyway. The package doc's import-cycle rationale was vacuous — `middlewares` never imported `agents` and never needed to. Inlining makes the spec's fixed middleware order literal, visible code. The summarization offload callback loses its `offload != nil` guard (the only caller always passes a closure).
- **Dead symbols deleted:** `ToolFilter.All()`/`denylistFilter.All()` (zero callers incl. tests), `agentConfig.Tools` (write-only — `composeAgent` passes `resolvedTools` directly), `IsTerminal()` (zero callers), `tool_call_requested`/`retrying` constants (never emitted, not in any spec scenario, unused by web). `reasoning_delta` is kept: never emitted today, but mandated by agent-runtime ("assistant text and reasoning SHALL be delivered as incremental delta events") — it marks a known `streamRun` gap, not dead weight. Never-populated payload fields (`Arguments`, `Latency`, `IsError`, …) are kept as the UI wire contract per the design contract's tool-call cards.
- **`config.go` dissolves by ownership:** `agentConfig`/`validateAgentConfig`/`DefaultSummarizationMargin` are runner-pipeline-only → `runner.go`; `FilesystemConfig`/`SkillsConfig`/`SummarizationConfig` are shared with composition's `Config` → `agent.go`; `DefaultMaxIterations` defaults in `Compose` → `agent.go`.
- **`instruction_composer.go` merges into `runner.go`:** single caller (`composeAgent`), single implementation, spec-frozen six-document order — no extension axis. The `InstructionComposer` interface survives as the test/injection seam.
- **Kept separate, on purpose:** `tool_registry.go` (the built-in tools registration surface — every future tool registers here, plus the `WithToolRegistry` seam), `model_factory.go` (the provider-type switch — each new provider type adds a case), `session_adapter.go` (implements Eino `CheckPointStore`/`SessionStore`; cross-package consumer in the Postgres conformance test), `jail.go` (446 lines + 481-line test) and `skills_resolver.go` (232 + 375) — merging either into `agent.go` would recreate the ~750-line monster the split just fixed.

### D6: No spec deltas — behavior-preserving refactor

`agent-runtime` and `agent-composition` requirements are behavior-only; neither mentions type or file names, and nothing observable changes. The proposal declares Capabilities: none and the change carries no `specs/` delta; type and file naming are design concerns (D1–D3), not requirements.

## Risks / Trade-offs

- **The rename touches `RouterOptions`, a shared wiring struct** — mechanical; the compiler surfaces every site (cli, router, two test files). Accepted.
- **Moving code between files in the same package cannot break references** — `runner.go` stays in `internal/agents`, so unexported symbols (`agentConfig`, `eventSerializer`, `extractAgenticText`, …) need no changes. Accepted.
- **Guard removal narrows a (theoretical) soft-failure path to a panic** — intended: silent empty history is worse than a loud failure, and the path is unreachable under the composition root.
- **`agent.go` shrinks to ~200 lines while `runner.go` carries ~520** — correct asymmetry: composition is a small pure contract, execution is the bulk of the runtime.

## Migration Plan

Pure refactor, no data migration. Order: create `runner.go` (verbatim + renames) → truncate `agent.go` → receiver/guard updates in `history.go` → wiring renames (cli, router, handlers) → test moves/renames → `go build ./... && go vet ./... && go test ./...` green with zero behavior change.
