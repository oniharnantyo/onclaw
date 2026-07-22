# Design — refactor agent assembly into a 5-phase builder

## The problem shape

`AssembleAgent` is a feature crossroads, not a long function that happened by accident. The
32-parameter signature is the headline symptom: it forces every caller and every test to
argue positionally, and it makes the cross-stage locals (`memMW`, `recency`, `sinkWrapper`,
`resolvedMem`) thread through one monolithic body.

## The 5-phase orchestrator

```go
func AssembleAgent(ctx context.Context, opts AssembleAgentOpts) (*Agent, error) {
	b := &agentBuilder{opts: opts}
	if err := b.resolveConfig(); err != nil { return nil, err }
	if err := b.buildPrompt(); err != nil { return nil, err }
	if err := b.buildTools(ctx); err != nil { return nil, err }
	if err := b.buildMiddleware(ctx); err != nil { return nil, err }
	return b.assemble(ctx)
}
```

| Phase | Method | Produces | Consumers |
|---|---|---|---|
| 1a resolveConfig | `b.resolveConfig()` | `b.resolvedMemory` | 2 (tool gating), 3 (memory toggles, callback flags) |
| 1b buildPrompt | `b.buildPrompt()` | `b.instruction` | 2 (floor guard), 4 (agentConfig) |
| 2 buildTools | `b.buildTools(ctx)` | `b.tools`, `b.enabledChecker` | 3 (fsToggle), 4 (agentConfig) |
| 3 buildMiddleware | `b.buildMiddleware(ctx)` | `b.handlers` (+ `b.memoryMiddleware`, `b.historyMiddleware`, `b.dispatcher`) | 4, runtime `Run()` |
| 4 assemble | `b.assemble(ctx)` | `*Agent` | — |

Phase 3 stays a single phase at the top level; its leaf steps (fs / summarization / history /
hooks / memory / skill) are private concerns. `buildMiddleware` dispatches to private
`buildSummarizationMiddleware` / `buildMemoryMiddleware` methods only for the two genuinely
complex middleware; the rest stay inline. The **top-level phase count stays at 5** — these
are private leaves, not new phases.

## Construction order ≠ execution order (the key simplification)

The handler chain's runtime order (`inputSafety, summarization, history, fs, fsToggle,
fsError, [memory], [skill], [hooks]`) is honored only when the `handlers` slice is assembled
at the end of Phase 3. Construction order is therefore free.

That freedom **eliminates the `memMW` forward-reference**: build the memory middleware
*before* the summarization callback, so the callback closes over an already-assigned
`b.memoryMiddleware` rather than a `var memMW` declared 120 lines early. The compaction
summary still flows callback → memory-MW via `b.memoryMiddleware.CompactionSummary` (the
single bridge for summary reuse); only the construction-order smell is removed.

## Summarization middleware — complexity prevention

The summarization area is the densest part of the function (construction L259–309 +
`handleSummarization` L634–760). The refactor must *simplify* it, not relocate it.

**Complexity sources today:** (1) a callback closure capturing ~12 values and
forward-referencing `memMW`; (2) the 12-field `handleSummarizationParams`; (3) cross-phase
locals `recency` (also fed to history MW) and `sinkWrapper` (also stored on the Agent).

**Simplifications:**

1. Kill the forward-reference via construction order (above).
2. Kill cross-method threading — `recency` and `sinkWrapper` become builder fields
   (`b.recency`, `b.sinkWrapper`): summarization writes them; history and `assemble` read them.
3. Isolate the callback in a private `buildSummarizationMiddleware` so it lives in one named
   place.

**Guardrails:**

- `handleSummarization` and `buildSummarizationConfig` stay **pure leaf functions** (already
  factored and tested via `export_test.go`); they are not inlined into the builder.
- Do **not** expand `handleSummarizationParams`. The deferred follow-up recorded in
  `harden-compaction-pipeline/design.md` — routing compaction-time extraction through
  `MemoryMiddleware.ExtractCompacted` — drops three fields (`ChatModel` / `MemoryStore` /
  `Embedder`). This refactor must preserve the seam that enables it (memory MW remains the
  extraction owner).

## ADR: onclaw stays on eino ADK, not a staged run-loop pipeline

**Context.** goclaw V3 replaces a monolithic `runLoop()` with an 8-stage procedural pipeline
(Context / Think / Prune / Tool / Observe / Checkpoint / Finalize). The question arose whether
onclaw should follow.

**Decision.** onclaw keeps eino ADK as the agent-loop owner and expresses run-loop concerns as
the composable middleware chain that `buildMiddleware` wires. It does not migrate to a
hand-rolled staged pipeline.

**Rationale.** goclaw's pipeline and onclaw's middleware differ in **who owns the loop**, not
in staging style. eino already ships ReAct iteration, streaming token routing, tool-call
parsing, and retries — and is already linked, so using its ADK costs nothing in binary/RAM.
Reimplementing that machinery on a ~2 GB SBC target adds code, RAM, and bug surface for no
binary benefit, contradicting onclaw's founding constraint.

**Consequences.** onclaw forgoes arbitrary mid-loop stage insertion (covered in practice by
`MaxIterations` + ctx cancellation). The pipeline's genuine strengths — legibility,
observability, deterministic timing — are borrowed *incrementally* instead: legibility via
this assembly refactor; observability via the existing `EventSink` (already wired at
`api/handler/chat.go:55`); deterministic timing via middleware-level fixes.

### goclaw stage → onclaw mechanism

| goclaw V3 stage | onclaw mechanism |
|---|---|
| ContextStage | `LoadPersonaContext` + workspace grounding + `EventSessionStart` |
| ThinkStage | `instruction` + `HistoryMiddleware` + `buildTools` filtering + eino model |
| PruneStage | `SummarizationMiddleware` (80% trigger) + `handleSummarization` extraction |
| ToolStage | eino tools node — **sequential only** (`ExecuteSequentially: true`) |
| ObserveStage | eino agent-loop internals |
| CheckpointStage | `MaxIterations` + ctx cancellation |
| FinalizeStage | `onStopFlush` (EventStop) + `LastTurnMeta` + `EventSink` |

### Deliberate differences (do not "fix")

- **Memory-flush timing** — goclaw flushes synchronously mid-loop in PruneStage; onclaw
  extracts in the compaction callback + a final end-of-turn `onStopFlush`. (Follow-up: harden
  the mid-loop path, not revert to end-of-turn-only.)
- **Tool parallelism** — onclaw is sequential-only.
- **Pruning strategy** — goclaw trims by ratio (30 / 50%); onclaw summarizes via LLM at 80%.

## Test-churn mitigation

Add `testAssembleOpts(t, overrides...)` so each of the 13 call sites sets only the field it
exercises; the rest are zero-valued. The `TestAssembleAndRunAgent_*` suite is the behavioral
safety net for the callback-timing and ordering invariants.

## Risks

- **Callback timing** — the compaction callback runs at turn time, after Phase 3 assigned
  `b.memoryMiddleware`. Covered by existing tests; add a regression test that an over-limit
  tool floor returns before any model call.
- **Ordering invariants** — floor guard before summarization; `recency.Load()` before its
  callback. A flat `b.step()` sequence makes these more visible, not less.
- **Mechanical discipline** — keep it a pure restructure; optionally two commits (restructure,
  then rename-to-full-names) for reviewability.
