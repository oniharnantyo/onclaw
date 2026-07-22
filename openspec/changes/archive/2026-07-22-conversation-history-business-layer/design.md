## Context

- 03.md's pattern is unambiguous: `session.Append(user)` → `session.GetMessages()` → `runner.Run(ctx, Normalize(history))` → collect reply → `session.Append(assistant)`. The runner is stateless about storage; the business layer owns the message list and the append/get/run/append loop.
- onclaw today pushes the entire loop inside one Eino middleware. `HistoryMiddleware` (`internal/agent/middlewares/history_middleware.go`) holds the `ConversationStore`, loads+injects history in `BeforeAgent` (`:72-154`), accumulates the turn from `state.Messages` in `AfterModelRewriteState` (`:156`) and `AfterAgent` (`:181`), and writes via `Store.AppendTurn` (`:269`) under `context.WithoutCancel`+5s (`:265`). `Agent.Run(ctx, userInput string)` (`agent.go:476`) builds a single-message `TypedAgentInput` and the entrypoint never sees the message list.
- The store layer is already eino-free and clean: `ConversationStore.AppendTurn` takes opaque JSON (`store.go:40`), and `*schema.AgenticMessage`↔JSON conversion happens in the agent layer. The refactor relocates the *orchestration* that drives the store; it does not build persistence from scratch.
- The `persist-turn-and-surface-lifecycle` change (separate, planning complete) exists only because persistence lives inside `AfterAgent`, which Eino does not call on abnormal terminals. Moving persistence to the business layer leaves a single `CommitTurn` seam that change can later invoke from the iterator's abnormal branches.

## Goals / Non-Goals

**Goals**
- A business-layer `SessionManager` owns history load and turn persist; the runner receives the assembled message list and does not load or persist history.
- `Agent.Run` takes `[]*schema.AgenticMessage`.
- All current behavior is preserved (turn-row shape, response-ID fallback, tokens, redaction, reasoning-strip, bounded replay, `is_summary`).
- Normal-terminal persistence semantics are unchanged (one row per completed turn). The separate persist-turn change extends this to abnormal terminals later.

**Non-Goals**
- Abnormal-terminal persistence and terminal-reason surfacing (separate change).
- Wiring `SaveSummary` / durable compaction cursor (currently unwired; follow-up).
- Touching memory flush, skills, hooks, filesystem, or summarization middleware.

## Decisions

### 1. Inject the committer at assembly; entrypoints cannot set iterator callbacks

`EventIterator` is an **interface** (`iterator.go:37-40`) and the concrete `eventIterator` is unexported; existing callbacks (`onStopFlush`, `onTurnError`) are wired *inside* `Agent.Run` (`agent.go:532-537`), not by callers. A design that had entrypoints do `it.(*eventIterator).onTurnComplete = …` would not compile. Therefore the `SessionManager` is supplied to `AssembleAgent` (as `Committer`) and the framework wires it into the `TurnCollector`. The entrypoint still owns `LoadHistory`, message assembly, draining, and `LastTurnMeta`.

### 2. Keep a framework `TurnCollector`; the streamed event stream cannot feed `CommitTurn`

In streaming mode the final assistant message arrives as chunks via `MessageStream`, so `eventIterator.collectedMsgs` (appended only for non-streaming `MessageOutput.Message`, `event_iterator.go:88-93`) never holds the final assistant message or its `ResponseMeta` (response ID + token usage). The accumulation with the correct fidelity is `bufferedMessages`, fed from framework `state.Messages` in `AfterModelRewriteState`/`AfterAgent`. So a framework-side collector must remain — it is the only component with state access — to gather the complete turn (including the final assistant message with `ResponseMeta`) and hand it to the business-layer committer. The collector holds no store and never writes; all write/replay logic moves to `SessionManager`. This is the honest 03.md split for a streaming ReAct agent: the framework collects and exposes the turn; the business layer owns storage.

### 3. Move replay (load) to the business layer; mark loaded messages persisted there

`SessionManager.LoadHistory` performs what `BeforeAgent` did: `store.LoadHistory` → unmarshal → mark `_onclaw_persisted=true` + `_onclaw_seq` → `stripReplayReasoning` (scrub-at-load, cache-stable) → `SanitizeSummaryMessage` on the summary row → derive `previousResponseID`. Because loaded messages are marked persisted at load, the framework `TurnCollector.accumulateNewMessages` (which skips system + persisted, dedupes by pointer) still buffers only the new turn's messages. `BeforeAgent` is removed entirely.

### 4. `TurnCollector` retains usage emission for the SSE context meter

`AfterModelRewriteState` currently emits per-call token usage to the `EventSink` (`history_middleware.go:163-176`), which the web UI context meter depends on. This stays in the framework `TurnCollector` (it observes model calls); only the persistence write moves. `Agent.SetEventSink` is retained and delegates to the collector.

### 5. `TurnCommitter` interface defined in `internal/agent`; no import cycle

`internal/agent` defines `type TurnCommitter interface { CommitTurn(context.Context, []*schema.AgenticMessage) (*store.TurnMeta, error) }`. `SessionManager` satisfies it structurally (Go interfaces are duck-typed), so `internal/conversation` need not import `internal/agent`, and `internal/agent` does not import `internal/conversation`.

### 6. Preserve normal-terminal-only trigger; leave abnormal to the separate change

The collector calls `Committer.CommitTurn` from `AfterAgent` (normal terminal), matching today's semantics. Abnormal terminals are not committed here — identical to current behavior (no regression). The separate `persist-turn-and-surface-lifecycle` change later wires the iterator's abnormal branches to call the same committer, which is idempotent.

### 7. Detached write and idempotency move with the write logic

`context.WithoutCancel(ctx)` + 5s timeout and the `_onclaw_persisted` / empty-buffer idempotency guards move into `SessionManager.CommitTurn`, since that is now where the write happens.

## Risks / Trade-offs

- **A framework collector remains.** Purists would move even accumulation to the business layer, but the final assistant `ResponseMeta` is only reachable from framework state in streaming mode. The trade-off: the framework collects+exposes; the business layer owns all storage and read/write logic. Net coupling to the store is removed from the framework.
- **Prefix-cache stability** depends on deterministic scrub-at-load reasoning strip; the logic moves verbatim to `LoadHistory` and is covered by a byte-stability test.
- **Usage-meter regression** if the `EventSink` emission is dropped; mitigated by keeping it in `TurnCollector.AfterModelRewriteState`.
- **Assembly overlap** with the same-day `2026-07-21-refactor-agent-assembly-phases` change (also edits `agentBuilder`/`AssembleAgent`); coordinate if both land together.
- **SSE behavior** is unchanged; only the source of `LastTurnMeta` and the `Run` argument change.