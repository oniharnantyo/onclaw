## 1. Options struct + builder skeleton

- [x] 1.1 `internal/agent/agent.go`: add `AssembleAgentOpts` struct (the 32 params as named
  fields) and `agentBuilder` (opts + accumulated state: `instruction`, `resolvedMemory`,
  `tools`, `enabledChecker`, `recency`, `sinkWrapper`, `memoryMiddleware`,
  `historyMiddleware`, `dispatcher`, `handlers`).
- [x] 1.2 Rewrite `AssembleAgent` as the 5-line orchestrator delegating to `b.resolveConfig`,
  `b.buildPrompt`, `b.buildTools`, `b.buildMiddleware`, `b.assemble`. Keep behavior identical.
- [x] 1.3 Add `testAssembleOpts(t, overrides...)` helper so callers set only the field they
  exercise.

## 2. Phase 1 — resolveConfig + buildPrompt

- [x] 2.1 `b.resolveConfig()`: parse `agentConf.MemoryConfig`, `Resolve(...)` →
  `b.resolvedMemory`.
- [x] 2.2 `b.buildPrompt()`: `LoadPersonaContext` + assemble `b.instruction`. (No merge with
  resolveConfig — different consumers.)

## 3. Phase 2 — buildTools + floor guard

- [x] 3.1 `b.buildTools(ctx)`: enabled-checker, `tools.Builtin` + MCP, memory-capability
  filter, agent-subset filter — as today.
- [x] 3.2 Floor-safety gate at the end of Phase 2: `estimateFloorTokens` →
  `FloorSafetyLimit` check, returning before middleware is built.

## 4. Phase 3 — buildMiddleware (construction order free)

- [x] 4.1 Filesystem MW inline (transcript loader, fs backend/shell, toggle, error).
- [x] 4.2 Private `b.buildMemoryMiddleware()` — built **before** the summarization callback.
- [x] 4.3 Private `b.buildSummarizationMiddleware()` — callback closes over the
  already-assigned `b.memoryMiddleware` (no `var memMW` forward declaration).
- [x] 4.4 `b.buildHistoryMiddleware()` (uses `b.recency`), hooks MW, skill MW.
- [x] 4.5 Assemble `b.handlers` in runtime order at the end: `inputSafety, summarization,
  history, fs, fsToggle, fsError, [memory], [skill], [hooks]`.

## 5. Phase 4 — assemble

- [x] 5.1 `b.assemble(ctx)`: `agentConfig` (+ handlers) → `adk.NewTypedChatModelAgent` →
  `Agent` struct (store `b.sinkWrapper`, `b.memoryMiddleware`, `b.historyMiddleware`) →
  start pruner.

## 6. Rename to full identifiers

- [x] 6.1 `memMW` → `memoryMiddleware`, `resolvedMem` → `resolvedMemory` (fields, params,
  locals); leave tight loop scopes idiomatic.

## 7. Caller migration

- [x] 7.1 `internal/cli/agent_session.go:290` `resolveAndAssemble`: build `AssembleAgentOpts`
  and call the new signature.
- [x] 7.2 Migrate the 13 test call sites in `internal/agent/*_test.go` to `testAssembleOpts`.

## 8. Verification

- [x] 8.1 `gofmt -w .` + `go vet ./internal/...`.
- [x] 8.2 `rtk go build ./...` green.
- [x] 8.3 `rtk go test ./internal/agent/... ./internal/cli/... -count=1` green; coverage ≥ 70%.
- [x] 8.4 Regression test: an over-limit tool floor returns before any model call.
- [x] 8.5 Manual: drive a conversation past the first compaction — turn completes,
  `CompactionSummary` still reused (no second LLM call), handler-chain order unchanged.
