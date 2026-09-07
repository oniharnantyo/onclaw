## Why

Agent composition — validate config, build the instruction, resolve tools, wire middlewares, construct the ADK agent — is scattered across `agent.go` (`composeAgent`), `config.go`, and `middleware_stack.go`, and the documented "nil means off" capability contract is not actually implemented (`buildAgentMiddlewares` dereferences `Filesystem`/`Summarization` unconditionally). The `agent_v2.go` boilerplate points at the destination — eino's `prebuilt/deep` pattern — but is incomplete and does not compile. Composition should be one pure function: data in, composed agent out, zero I/O.

## What Changes

- Complete `internal/agents/agent_v2.go` as a pure composer: `Compose(ctx, *Config) (adk.TypedResumableAgent[*schema.AgenticMessage], error)` — no stores, no database, no disk reads; the caller supplies every input.
- `Config` is flattened, deep-style: `Name`, `Description`, `Instruction` (caller-composed string), `ChatModel` (caller-built), `Tools` (caller-resolved from registry + denylist), `MaxIterations`, plus capability pointers `Filesystem` / `Skills` / `Summarization` where nil means off.
- `buildMiddlewares` becomes genuinely conditional — capabilities attach only when their config pointer is non-nil, preserving the verified order patchtoolcalls → reduction → summarization → skill → filesystem.
- `validateConfig` fails fast: model, name, and instruction required; `MaxIterations` zero falls back to the package default; summarization requires filesystem (the offload target directory must exist).
- Remove the dead boilerplate: `buildPrompt`/`buildTools` stubs, the unused `modelFactory` field, the stray `agent.Run()` call, and the `return nil, nil` that discards the constructed agent.
- The existing engine's `composeAgent` step delegates to the composer so there is one composition path; the engine keeps load, resolve, and execution (runner + event streaming) untouched.

## Capabilities

### New Capabilities
- `agent-composition`: the pure composition contract — building an executable ADK agent from caller-supplied data with selective, nil-means-off capabilities and fail-fast validation.

### Modified Capabilities

## Impact

- `internal/agents/agent_v2.go` — completed (main work); `internal/agents/agent.go` — `composeAgent` becomes config assembly + delegation; `internal/agents/middleware_stack.go` — absorbed into the composer's builder and deleted; `internal/agents/config.go` — unchanged (engine-side per-run config).
- No store, schema, API, or frontend changes. No eino version change (pinned `v0.10.0-alpha.28`).
- Existing engine e2e tests remain the regression harness for middleware ordering and event-stream parity.
