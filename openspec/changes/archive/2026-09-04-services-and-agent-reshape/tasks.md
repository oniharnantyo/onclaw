## 1. Services layer

- [x] 1.1 Create `internal/services`; move `internal/auth/service.go` → `services/auth.go` with renamed types (`Service` → `AuthService`, `NewService` → `NewAuthService`); move tests
- [x] 1.2 Move remaining auth files with domain prefixes: `provider.go` → `auth_provider.go`, `password_provider.go` → `auth_password_provider.go`, `password.go` → `auth_password.go`, `issuer.go` + `jwt.go` → `auth_jwt.go` (TokenIssuer, JWTIssuer, JWTConfig, PasswordHasher keep their names)
- [x] 1.3 Move `internal/modelcatalog/catalog.go` → `services/modelcatalog.go` (`Service` → `ModelCatalog`, `Options` → `ModelCatalogOptions`, `NewService` → `NewModelCatalog`); move tests
- [x] 1.4 Create `services/doc.go` orienting the two domains and stating the prefix convention
- [x] 1.5 Re-point auth consumers to `services`: `internal/bootstrap`, `internal/cli/{server,user,drivers}.go`, `internal/server/{router,middleware}.go`, `internal/server/handlers/{auth,admin_users}.go`, and their tests
- [x] 1.6 Re-point modelcatalog consumers: `internal/server/router.go`, `internal/cli/server.go`, `internal/server/handlers/{agents,providers,workspaces}.go`, `internal/server/models_test.go`
- [x] 1.7 Delete `internal/auth/` and `internal/modelcatalog/`; verify no references remain (`grep -r "internal/auth\|internal/modelcatalog"`)
- [x] 1.8 Phase gate: `go build ./... && go vet ./... && go test ./...` green

## 2. Promptgen extraction

- [x] 2.1 Create `internal/promptgen`; move `agents/service.go` → `promptgen/service.go` (Service: Generate / GenerateForCreate / Sweep; options renamed `Option` → `Option`, `WithTimeout` kept, `WithAgentPromptGeneratorModelFactory` → `WithModelFactory`); move `service_test.go` and `service_create_test.go`
- [x] 2.2 Move `agents/factory.go` → `promptgen/model_factory.go` (ModelFactory type, AgentPromptGeneratorModelFactory, generationOptions)
- [x] 2.3 Split `agents/prompts.go`: generation prompt text, persona framing, JSON schema, structured-output format, Build* message builders, GeneratedPrompts, parsing/regex → `promptgen/prompts.go`; `BasePrompt` embed + document-name constants stay in `agents` (they seed the runtime L1 document)
- [x] 2.4 Add `promptgen → agents` import for prompt-document I/O (`WritePromptDocuments`, `ReadPromptDocuments`); verify no reverse import
- [x] 2.5 Re-point consumers: `internal/cli/server.go` and `internal/server/router.go` construct `promptgen.NewService`, `RouterOptions.AgentService` field type → `*promptgen.Service`; update handler usage and `handlers_test.go` fakes
- [x] 2.6 Phase gate: `go build ./... && go vet ./... && go test ./...` green

## 3. Runtime reshape

- [x] 3.1 Create `agents/agent.go`: `Agent` struct + `NewAgent` (positional store deps unchanged) + `Run` decomposed into `load → resolve → composeAgent → execute` (deep.go-as-heart structure); existing engine tests pass against the decomposed form
- [x] 3.2 Add `agentConfig` with nil-means-off capability fields (`Filesystem *FilesystemConfig`, `Skills *SkillsConfig`, `Summarization *SummarizationConfig`), identity, `ChatModel`, tools, `MaxIterations`; add `validateAgentConfig`
- [x] 3.3 Replace `middleware_stack.go` with `buildAgentMiddlewares` preserving ordering (patchtoolcalls → reduction → summarization → skill → filesystem); rehome `skillBackend` adapter and `offloadTranscript` next to their consumers; `middlewares/` subpackage unchanged
- [x] 3.4 Set `MaxIterations` from a named default constant on the ADK chat-model agent config (bounded execution loop; spec scenario: runaway loop terminates, converging turn unaffected)
- [x] 3.5 Split `engine.go` → `events.go` (TranscriptEvent kinds/payloads, ExecRequest) + `stream.go` (EventStream); delete the `Engine` interface; rename `Engine` struct → `Agent`; update e2e tests to hold the concrete `*Agent`
- [x] 3.6 Refile: `fs_jail.go` → `jail.go`, `workspace.go` → `promptdocs.go`, `agentic_factory.go` → `model_factory.go`
- [x] 3.7 Create `agents/doc.go`: runtime-only package orientation (engine, jail, skills, docs) and the one-way import rule
- [x] 3.8 Phase gate: `go build ./... && go vet ./... && go test ./...` green

## 4. Provider policy dedupe

- [x] 4.1 Extract `stripVersionPath` → `providers.StripVersionPath` in `internal/providers` (with the OpenRouter default-endpoint decision alongside it)
- [x] 4.2 Consume from both factories (`agents/model_factory.go`, `promptgen/model_factory.go`); remove local copies
- [x] 4.3 Phase gate: `go build ./... && go vet ./... && go test ./...` green

## 5. Final verification

- [x] 5.1 Full unit suite: `go test ./...`
- [ ] 5.2 Integration suite against PostgreSQL (`go test -tags=integration ./...` with `TEST_DATABASE_URL`)
- [x] 5.3 Convention sweep: no compatibility aliases or re-export shims; no `if x != nil` defensive guards on injected deps introduced; package names appear exactly once per concept
- [ ] 5.4 `openspec validate services-and-agent-reshape --strict` passes
