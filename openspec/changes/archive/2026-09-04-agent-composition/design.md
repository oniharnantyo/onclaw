## Context

See proposal.md — Why. Current state shaping the approach:

- Composition logic lives in `agent.go` `composeAgent` (validate → instruction → middlewares → construct), `config.go` (`agentConfig` + `validateAgentConfig`), and `middleware_stack.go` (`buildAgentMiddlewares`). `middleware_stack.go` dereferences `cfg.Filesystem` and `cfg.Summarization` unconditionally even though `config.go` documents "nil means off" — the contract is not implemented; the engine's `resolve` phase always sets all three capabilities.
- `agent_v2.go` is an incomplete boilerplate: it does not compile (`MaxIterations` is `*int` on the domain entity but `int` in the ADK config; `agent.Run()` is called without arguments) and it discards the constructed agent (`return nil, nil`).
- eino is pinned at `v0.10.0-alpha.28`. The upstream `prebuilt/deep` at `alpha.30` is the pattern reference (validate → default instruction → build handlers → construct → return `adk.TypedResumableAgent[M]`); design D3 of `services-and-agent-reshape` already adopted the pattern without importing the package.
- Sole production consumer today: `internal/cli/server.go` constructs the engine (construct-and-discard wiring until the SSE handler lands).

## Goals / Non-Goals

**Goals:**
- One pure composition entry point: data in, composed agent out, zero I/O
- Make "nil means off" real — capabilities attach only when configured
- Single composition code path: the engine delegates its compose step to the composer
- Delete absorbed indirection (`middleware_stack.go`) and the dead boilerplate pieces

**Non-Goals:**
- No execution changes: load/resolve/execute, event streaming, session adapter, `events.go`/`stream.go` untouched
- No `prebuilt/deep` import and none of its built-ins
- No changes to `middlewares/`, `tools/`, `systemskills/` subpackages
- No store, schema, API, or frontend changes; no eino version bump
- No file renames in this change (`agent_v2.go` keeps its name; engine type and layout unchanged). The exported surface is already timeless (`Compose` / `Config`); the transient `v2` marker in the filename retires later, when the engine's execution path moves out and this file becomes the package's only agent construction path

## Decisions

### D1: Package function, not a struct method
`Compose(ctx context.Context, cfg *Config) (adk.TypedResumableAgent[*schema.AgenticMessage], error)` — mirroring `deep.NewTyped`. The boilerplate's `AgentV2` struct exists only to hold `modelFactory`, which is dead once `Config` carries a prebuilt `ChatModel`; an empty struct and a `NewAgentV2` constructor would be ceremony. The name is a verb because the package already has many `New*` constructors (`NewAgent`, `NewToolRegistry`, `NewInstructionComposer`) and a bare `New` would be ambiguous against `NewAgent`. Alternative (keep the struct + method) rejected: stateless wrapper adds nothing.

### D2: Flatten `Config`; drop the domain entity field
The boilerplate carries `Agent *domain.Agent`; the design drops it. The caller already holds the entity and copies what it needs (`Name`, `Description`, dereferenced `MaxIterations`). This matches deep (whose `Config` has no entity concept), avoids coupling the composer to fields it never reads (`Effort`, `Temperature`, …), and fixes the `*int`→`int` compile error by deletion — the caller derefs with the default. Alternative (keep the entity, deref inside) rejected: one less caller line is not worth the coupling.

### D3: Data-vs-behavior rule for what Config carries
Things only the caller can know arrive pre-built: `ChatModel` (constructed with decrypted credentials) and `Tools` (resolved from the registry with denylist applied via `ResolvedTools`). Things derivable from plain data are built inside: the filesystem jail from `AgentDir`, the skills resolver from `OnClawDir`/tenant/agent coordinates. This is why `Config` carries directory coordinates rather than deep-style `Backend` fields — accepting constructed backends would leak jail construction and eino filesystem types to every call site. Alternative (deep-purist `Backend` fields) rejected for application code.

### D4: `buildPrompt` and `buildTools` are deleted, not completed
deep's instruction is a caller-composed string; so is ours. The engine keeps `InstructionComposer` and `ResolvedTools` on its side of the boundary and hands over finished values. This preserves the `agent-runtime` instruction-composition requirement unchanged — per-execution composition still happens, one layer up. Prompt-document I/O stays out of the composer.

### D5: Conditional middleware builder; summarization requires filesystem
`buildMiddlewares` branches on capability presence and preserves the verified order patchtoolcalls → reduction → summarization → skill → filesystem (patchtoolcalls unconditional; jail built once and shared by reduction + filesystem; skill backend wraps the three-tier resolver; summarization offload writes `transcript.md` into the jailed dir). Summarization configured without filesystem is a validation error: its offload target would be undefined. The engine's `resolve` always sets filesystem, so no existing production path regresses. Iteration cap: 0 → `DefaultMaxIterations` (25) at the composer boundary. `GenModelInput` is not set — plain `TypedChatModelAgent` applies `Instruction` natively; deep sets it only because it injects system messages itself.

### D6: Engine delegates its compose step
The engine's `composeAgent` becomes config assembly (instruction from the composer, tools from `ResolvedTools`, capability structs from resolve output) plus a `Compose` call. `middleware_stack.go` is deleted — absorbed into the composer's builder. One code path keeps middleware ordering from drifting; the existing engine e2e tests remain the regression harness. The `tool_registry.go` confinement promise ("callers outside internal/agents never reference `tool.BaseTool`") still holds because the resolve phase stays in-package; the promise's comment is updated to name the composer boundary.

## Risks / Trade-offs

- [Middleware semantics drift during the rewrite] → engine delegates rather than duplicating; ordering is asserted by unchanged e2e tests; the order constant lives in one builder
- [New validation failure mode: summarization without filesystem] → unreachable from the engine (resolve always sets filesystem); composer-level tests cover the rejection explicitly
- [`tool.BaseTool` in the exported `Config` widens the eino-type surface] → only reachable in-package today (resolve is the sole caller); confinement comment updated; revisit if a future SSE service composes agents directly
- [Two composition surfaces during the transition commit] → single phased change: composer lands with delegation in the same change, so no window with two live paths

## Migration Plan

One change, three steps, each compiling and green (`go build ./... && go vet ./... && go test ./...`):
1. Complete `agent_v2.go` (`Config`, `validateConfig`, `buildMiddlewares`, `Compose`) + composer unit tests
2. Delegate the engine's `composeAgent` to `Compose`; delete `middleware_stack.go`; update the confinement comment
3. Full suite + integration suite against `DATABASE_URL`

Rollback is `git revert`; no data or schema changes.

## Open Questions

None — naming (`Compose`), Config shape, and the summarization/filesystem coupling were resolved during exploration.
