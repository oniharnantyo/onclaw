## Why

`internal/agent/agent.go::AssembleAgent` (lines 64–438) is the agent's single assembly
crossroads: a ~375-line function taking **32 positional parameters**. Each memory feature
(core / dreaming / graph / episodic / KG), the filesystem-middleware adoption, and the
chat-stop work landed here as one more surgical toggle — so the function accreted rather
than grew by design. Three concrete pains:

- **Illegible tests.** 13 call sites pass positional `nil, nil, …, 0, 0, nil, 3`; no reader
  can tell which argument is which without counting against the signature.
- **A caller that fights the signature.** The sole production caller
  (`internal/cli/agent_session.go:290`, `resolveAndAssemble`) tears config structs apart to
  feed 32 positional args.
- **A smeared invariant.** `memMW` is declared at L267 purely so the summarization callback
  (L282) can write `memMW.CompactionSummary`; it is assigned ~120 lines later at L386.

No spec contracts assembly (verified: `openspec/specs/` has zero `AssembleAgent` mentions),
so this is a behavior-preserving internal refactor.

## What Changes

Collapse the 32 parameters into an `AssembleAgentOpts` struct and restructure the body into
a **5-phase builder** (`agentBuilder`), with construction order decoupled from the runtime
middleware-chain order:

1. `resolveConfig()` — `resolvedMemory` (operational config; consumed by phases 2–3).
2. `buildPrompt()` — `instruction` (model-facing prompt; consumed by phases 2 + 4). Phase 1
   splits because the two outputs have different consumers; "context" is the wrong unifier
   and collides with `ctx context.Context`.
3. `buildTools(ctx)` — built-in + MCP tools, memory-capability + agent-subset filtering, and
   the input-floor safety gate.
4. `buildMiddleware(ctx)` — the handler chain (fs / summarization / history / hooks / memory
   / skill). The `memMW` forward-reference is eliminated by building the memory middleware
   before the summarization callback.
5. `assemble(ctx)` — `agentConfig` + handlers → eino agent → `Agent` struct + pruner.

Identifier names are spelled out (`memoryMiddleware`, not `memMW`; `resolvedMemory`, not
`resolvedMem`) per the new readability conventions recorded in `CLAUDE.md`.

## Capabilities

### Modified Capabilities

- `agent-core`: add requirements that contract two previously-implicit assembly invariants the
  refactor preserves — the fixed handler-chain order (a runtime property independent of
  middleware construction order) and the input-floor safety gate that fails assembly before any
  model call. The refactor changes no observable behavior; these requirements retroactively
  codify existing behavior so it is protected from regression.

## Impact

**Affected code:**

- `internal/agent/agent.go` — the builder, `AssembleAgentOpts`, and the renamed fields.
- `internal/cli/agent_session.go:290` — `resolveAndAssemble`, the only production caller,
  updated to the opts struct. The four entry points (`run.go`, `chat.go`, `serve_cmd.go`,
  the API chat handler) call `resolveAndAssemble`, so they are untouched.
- 13 test call sites in `internal/agent/*_test.go` — migrated via a `testAssembleOpts`
  helper that sets only the field each test exercises.
- `internal/agent/agent_export_test.go` — bridge re-exports move with their helpers; minimal.

**Affected systems:** agent assembly only. The `*Agent` runtime contract (`Run`,
`SetEventSink`, `ContextWindow`, `AgentName`, `LastTurnMeta`, defined as an interface at
`internal/api/service/types.go:14-17`) is unchanged, so the API layer, the SSE handler, and
all mocks are unaffected.

**Dependencies:** none new. Reuses existing helpers (`buildSummarizationConfig`,
`estimateFloorTokens`, `handleSummarization`, `buildTranscriptPath`, `buildTranscriptLoader`).

**Non-goals:** no behavior change; no run-loop restructuring (see the ADR in `design.md` —
onclaw stays on eino ADK); no migration toward the goclaw staged run-loop pipeline.
