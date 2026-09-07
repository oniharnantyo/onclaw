## 1. Composer implementation (agent_v2.go)

- [x] 1.1 Replace the boilerplate: flattened `*Config` (Name, Description, Instruction, ChatModel, Tools, MaxIterations, Filesystem/Skills/Summarization pointers), delete `AgentV2` struct, `modelFactory` field, `buildPrompt`, `buildTools`
- [x] 1.2 Implement `validateConfig`: model, name, instruction required; summarization requires filesystem; descriptive errors
- [x] 1.3 Implement conditional `buildMiddlewares`: patchtoolcalls always; jail built once from `AgentDir` shared by reduction + filesystem when Filesystem present; skill backend over the three-tier resolver when Skills present; summarization with offload-to-transcript.md callback when Summarization present; fixed order patchtoolcalls → reduction → summarization → skill → filesystem
- [x] 1.4 Implement `Compose(ctx, *Config) (adk.TypedResumableAgent[*schema.AgenticMessage], error)`: validate → build → construct; MaxIterations 0 → DefaultMaxIterations; ToolsConfig wired from supplied tools; no `agent.Run()`, return the constructed agent

## 2. Composer tests

- [x] 2.1 Validation tests: missing model, empty name, empty instruction, summarization without filesystem — each rejects with a descriptive error and constructs nothing
- [x] 2.2 Capability tests: all-absent composes a working agent with no capability middlewares; filesystem absent → no file tools on the surface; each present capability attaches its behavior; full stack asserts handler order patchtoolcalls → reduction → summarization → skill → filesystem
- [x] 2.3 Iteration default test (0 → 25) and deterministic composition test (same config twice → same surface)

## 3. Engine delegation

- [x] 3.1 Rewrite the engine's compose step as config assembly (instruction from InstructionComposer, tools from ResolvedTools, capability structs from resolve output) plus a `Compose` call
- [x] 3.2 Delete `middleware_stack.go`, moving `skillBackend` and `offloadTranscript` into `agent_v2.go` (single-file composer, no new files)
- [x] 3.3 Update the confinement comment in `tool_registry.go` to name the composer boundary as the eino-type edge
- [x] 3.4 Run the engine e2e tests — middleware ordering and event-stream parity must pass unchanged

## 4. Verification

- [x] 4.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 4.2 Integration suite against `DATABASE_URL` green
- [x] 4.3 Grep for stale references: `buildAgentMiddlewares`, `AgentV2`, `NewAgentV2` — zero hits outside history
