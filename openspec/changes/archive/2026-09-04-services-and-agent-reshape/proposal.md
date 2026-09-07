## Why

`internal/agents` mixes two unrelated subsystems — prompt generation and the runtime engine — in one flat namespace with ambiguous file names (`service.go`, `workspace.go`), a 10-step `Run` god-method, ~250 lines of duplicated provider-switch logic, and an `Engine` interface that has zero production consumers. `internal/auth` and `internal/modelcatalog` each dissolve wholesale into a shared application-services package per the agreed services-layer convention. Before the runtime grows (chat SSE, cron, channels), the package needs disciplined seams: one composition entry point, config-driven capabilities, and files that announce their subsystem.

## What Changes

- **New `internal/services`** — `internal/auth` (AuthService, provider registry, password provider, argon2id hasher, JWT issuer) and `internal/modelcatalog` (ModelCatalog) dissolve into it as domain-prefixed files; both packages are deleted. **BREAKING** (import paths; internal-only, compile-level)
- **New `internal/promptgen`** — the prompt generation subsystem extracted from `internal/agents`: generation Service (Generate / GenerateForCreate / Sweep), generation model factory (standard eino-ext models), prompt text + JSON schema + parsing. `promptgen` may import `agents` (prompt-document I/O); never the reverse
- **`internal/agents` becomes runtime-only**, restructured on the eino `prebuilt/deep` *pattern* (no `prebuilt/deep` import — composition stays on `adk.NewTypedChatModelAgent` and `adk/middlewares/*`):
  - `agent.go` as the package heart: `Engine` struct + `NewEngine` (positional store deps per project convention) + `Run` decomposed into `load → resolve → composeAgent → execute`
  - `agentConfig` (per-execution composition surface, capability fields nil-means-off), `validateAgentConfig`, `buildAgentMiddlewares` — the deep `TypedConfig`/`validateTypedConfig`/`buildTypedBuiltinAgentMiddlewares` discipline
  - `engine.go`'s consumer-less `Engine` interface deleted; its live content splits into `events.go` (TranscriptEvent protocol) and `stream.go` (EventStream machinery)
  - refiles: `fs_jail.go → jail.go`, `workspace.go → promptdocs.go`, `agentic_factory.go → model_factory.go`; package doc (`doc.go`) orienting the runtime
- **New behavior: bounded execution loop** — `MaxIterations` set on the ADK chat-model agent config (currently uncapped)
- **`providers.StripVersionPath`** — base-URL version-path normalization extracted to `internal/providers`, consumed by both the runtime (agentic) and promptgen (generation) model factories, deduplicating the switch policy
- **No compatibility aliases** — consumers re-pointed directly (~22 files): `cli/server.go`, `server/router.go`, handlers (agents, workspaces, auth, admin_users, providers), bootstrap, middleware, and their tests

## Capabilities

### New Capabilities

- None — this is an internal reorganization; existing behavior is preserved.

### Modified Capabilities

- `agent-runtime`: adds the requirement that an agent execution loop is bounded by a maximum iteration count (previously uncapped); all other runtime requirements are behavior-preserving and only relocate code.

## Impact

- **Packages**: `internal/agents` (reshaped), `internal/auth` + `internal/modelcatalog` (deleted), `internal/services` + `internal/promptgen` (new), `internal/providers` (+StripVersionPath)
- **Consumers**: `internal/cli` (server, user, drivers), `internal/server` (router, middleware), `internal/server/handlers` (agents, workspaces, auth, admin_users, providers), `internal/bootstrap`, plus their tests — import and qualifier updates only
- **Dependencies**: none added or removed in `go.mod`; eino ADK + eino-ext usage unchanged, pinned at `v0.10.0-alpha.28` (deep prebuilt verified byte-identical through `alpha.30` and intentionally not imported)
- **Behavior**: only the new iteration cap; prompt generation, authentication, model catalog, and runtime streaming semantics unchanged
