## Context

`internal/agents` (13 source files, ~3.6k lines) holds two subsystems: prompt generation (`service.go`, `factory.go`, `prompts.go`) and the runtime engine (`engine.go`, `engine_impl.go`, `middleware_stack.go`, plus jail, skills, tools, session adapter). `internal/auth` and `internal/modelcatalog` each package a service with its ports. The runtime composes per execution on the eino ADK (`adk.NewTypedChatModelAgent` + `adk/middlewares/*`, pinned `v0.10.0-alpha.28`); the composition is currently a 10-step `Run` plus a hand-assembled middleware stack, behind an `Engine` interface with zero production consumers (the composition root constructs and discards the engine; chat SSE is deferred). Consumers are few: `cli/server.go`, `server/router.go`, and a handful of handlers touch only `NewService`, `NewEngine`, `SeedWorkspace`, `WritePromptDocument`, `ReadPromptDocuments`, `SanitizeError`.

Project conventions that bind this design: injected dependencies are positional store sub-interfaces (no fat dependency configs, no nil guards), behavior knobs are functional options, extension points are interfaces, no compatibility aliases.

## Goals / Non-Goals

**Goals:**
- One subsystem per package: `internal/services` (auth, modelcatalog), `internal/promptgen` (prompt generation), `internal/agents` (runtime only)
- Runtime composition restructured on the eino `prebuilt/deep` *pattern* — one entry point, a config surface with nil-means-off capabilities, validate-then-build helpers — without importing `prebuilt/deep`
- Remove the speculative `Engine` interface; keep the execution protocol types
- Single-source the provider base-URL policy shared by both model factories
- Make the package self-orienting (package doc, files named for their responsibility)

**Non-Goals:**
- No `prebuilt/deep` import and none of its built-ins (subagents/task tool, write_todos, shell, background tasks) — those become future `agentConfig` capabilities if products demand them
- No behavior change beyond the new iteration cap; no store/schema, API, or frontend changes
- No reworking of `middlewares/`, `tools/`, `systemskills/` subpackages (already correctly layered)

## Decisions

### D1: Whole-domain dissolution for auth and modelcatalog
`internal/auth`'s `NewService` wires an unexported `registry` and `passwordProvider`; lifting only the Service type out would force exporting ports or splitting the package. The whole domain relocates instead: `services/auth.go`, `services/auth_provider.go`, `services/auth_password_provider.go`, `services/auth_password.go`, `services/auth_jwt.go`, `services/modelcatalog.go`. Type names de-collide by prefix: `AuthService`, `ModelCatalog` (+`ModelCatalogOptions`), keeping the project's one-name-per-concept rule. Alternative considered (ports stay in `auth`, service moves) rejected: it cuts a cohesive package in half and exports types only `services` uses.

### D2: Prompt generation gets its own package, not a slot in services
`promptgen` is a ~750-line subsystem (service + generation model factory + prompt text/JSON-schema/parsing + sweep), not a thin orchestration service; package-by-cohesion beats package-by-uniformity. `promptgen.Service` names without stutter. Import direction is one-way: `promptgen → agents` for prompt-document I/O (`WritePromptDocuments`/`ReadPromptDocuments`), never the reverse; document I/O stays in `agents` (`promptdocs.go`) because the runtime's instruction composer reads the same files and handlers call `SeedWorkspace` directly. The `BasePrompt` embed (`prompts/AGENTS.md`) stays in `agents` — it is the runtime L1 document, not generation machinery; `prompts.go` splits along that line when moving.

### D3: Adopt the deep pattern, not the deep package
Evidence: between `alpha.30` and `main`, upstream deleted the entire `Background`/durable-subagent config surface from the prebuilt while the plug-in fields (`Backend`, `Handlers`, `ToolsConfig`) survived — the prebuilt churns around conveniences and stabilizes around extension points. OnClaw therefore keeps composing on `adk.NewTypedChatModelAgent` + `adk/middlewares/*` (untouched, already verified by the e2e suite) and copies the discipline:
- `agentConfig` (unexported — deep exports `Config` because it is a library; this is application code): identity, `ChatModel`, tools, capability fields (`Filesystem`/`Skills`/`Summarization` — nil = off), `MaxIterations`
- `validateAgentConfig` mirroring `validateTypedConfig` (fail fast: nil model, empty instruction)
- `buildAgentMiddlewares` mirroring `buildTypedBuiltinAgentMiddlewares` (patchtoolcalls always; reduction iff Filesystem; summarization iff Summarization; skill iff Skills; filesystem iff Filesystem)
- `composeAgent` = validate → build → construct, returning the composed agent for `Run`'s execute phase
`Run` becomes `load → resolve (tenant state → agentConfig) → composeAgent → execute (runner + streamRun)`. The god-method dissolves along deep's own seams.

### D4: Delete the `Engine` interface; keep the protocol
The interface has one implementation and zero production consumers; the package's real test seam is functional options (`WithAgenticModelFactory` fakes the LLM boundary inside the *real* engine — a mocked `Engine` would bypass everything worth testing). Go puts interfaces at consumers; when the SSE handler lands it declares the seam it needs and the concrete type satisfies it implicitly. `engine.go`'s live content survives as `events.go` (event kinds, payloads, `ExecRequest`) and `stream.go` (`EventStream`). The concrete type keeps the name `Engine` (avoids `agents.Agent` stutter and collision with the `domain.Agent` entity); its file is `agent.go` per the deep.go-as-heart convention. The composition root keeps its construct-and-discard wiring until SSE lands.

### D5: Two-tier construction split
Dependencies stay positional on `NewEngine(...)` per project convention — a Config there would be the forbidden fat dependency struct. The Config pattern applies at the per-run composition seam, where the inputs are *capabilities resolved from tenant state*, not injected dependencies. `MaxIterations` and `ModelRetryConfig` are ADK-level agent-config fields (deep merely forwarded them upstream — verified in main's `deep.go`), so OnClaw sets them directly; the retry path wires the already-existing `TranscriptEventRetrying` event kind.

### D6: Provider URL policy into `internal/providers`
Both model factories (runtime agentic, promptgen generation) duplicate the six-provider switch including base-URL handling. The SDK layers genuinely differ (`model.BaseChatModel` vs agentic models) so the switches stay, but the URL/version-path policy (`stripVersionPath`, OpenRouter default endpoint) single-sources as `providers.StripVersionPath` in `internal/providers`, where provider domain knowledge already lives.

## Risks / Trade-offs

- [Large mechanical diff across ~22 consumer files] → single phased change with per-phase gates (`go build ./... && go vet ./... && go test ./...`); tests move with their subjects so each phase compiles green before the next
- [Middleware ordering/semantics drift during the compose rewrite] → the existing engine e2e tests are the harness; ordering (patchtoolcalls → reduction → summarization → skill → filesystem) is preserved explicitly in `buildAgentMiddlewares`, and event-stream parity is asserted by the unchanged `streamRun` mapping
- [`MaxIterations` default too low truncates legitimate long tool loops] → generous default constant (25), named in one place, trivially tunable; spec scenarios cover both cap-hit and normal completion
- [`prompts.go` split moves generation machinery while `BasePrompt` embed stays] → split is mechanical (embed + `BasePrompt` + document-name constants remain in `agents`; generation text, schema, parsing, build helpers move); both sides are covered by existing tests (`workspace_test.go` stays, `service_test.go` moves)
- [Temporarily larger `internal/agents` during phase ordering if refiles land before excision] → task order: create `services`/`promptgen` and excise first, reshape the slimmed runtime second

## Migration Plan

Single change, four task phases, each ending compiling and green:
1. **Services layer** — create `internal/services`, dissolve `internal/auth` + `internal/modelcatalog`, re-point consumers (bootstrap, cli, server, handlers, tests); delete the two packages
2. **Promptgen extraction** — create `internal/promptgen` from `agents`' generation subsystem, split `prompts.go` along the BasePrompt line, re-point router/handlers/cli
3. **Runtime reshape** — `agent.go` (+`agentConfig`/validate/build/compose), `events.go`/`stream.go`, delete the `Engine` interface, add `MaxIterations`, refiles (`jail.go`, `promptdocs.go`, `model_factory.go`), package doc
4. **Provider policy dedupe** — `providers.StripVersionPath`, both factories consume it

Rollback is `git revert`; no data, schema, API, or configuration changes. Post-merge verification: full unit suite plus integration suite against `DATABASE_URL`.

## Open Questions

- Exact `MaxIterations` default value (working proposal: 25) — safe to tune at implementation time; does not affect spec or structure
