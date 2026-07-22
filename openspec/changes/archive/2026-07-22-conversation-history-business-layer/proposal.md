## Why

`docs/eino_agent_best_practice/03.md` is explicit that conversation history is a **business-layer** concern that lives **outside `runner.Run()`**: the business layer owns a Session/Store, assembles the message list (`history + new user message`), passes it to the runner, collects the reply, and appends it back. The framework runner only processes the message list it is given — it neither loads nor persists history.

onclaw inverts this. `HistoryMiddleware` (`internal/agent/middlewares/history_middleware.go`) is simultaneously an Eino framework middleware **and** the owner of persistence: `BeforeAgent` loads and injects history, `AfterModelRewriteState`/`AfterAgent` accumulate the turn from framework state and call `Store.AppendTurn`. `Agent.Run(ctx, userInput string)` takes only the new message, so the business layer (CLI/API entrypoints) never owns the message list. This coupling is the root cause of the separate `persist-turn-and-surface-lifecycle` change: Eino's `AfterAgent` fires only on successful terminals and exposes no finalize hook, so error/interrupt/cancel turns silently lose history — a symptom of owning persistence inside the framework's lifecycle rather than in the business layer that observes the whole turn.

## What Changes

- **Introduce a business-layer owner `SessionManager`** (`internal/conversation/`) that holds the `ConversationStore` and owns all read/write logic: `LoadHistory` (replay: unmarshal, mark persisted, strip reasoning, sanitize summary, derive `previousResponseID`) and `CommitTurn` (redact, strip reasoning, extract response-ID + token usage, marshal, `AppendTurn` under a detached context, record `lastTurnMeta`, idempotent).
- **Change `Agent.Run` to receive the assembled message list** (`[]*schema.AgenticMessage`) instead of a user-input string; the entrypoint assembles `history + new user message` and the runner no longer loads history.
- **Reduce `HistoryMiddleware` to a framework `TurnCollector`** that keeps only what requires framework state access: accumulate the turn's new messages from `state.Messages`, emit per-call token usage via the `EventSink`, and at terminal hand the complete turn to the injected `SessionManager` committer. It holds no store and never writes.
- **Move the entrypoints (CLI REPL, CLI run, API) into the business-layer driving seat**: build the `SessionManager`, call `LoadHistory`, assemble the message list, `Run(messages)`, drain the iterator for SSE, and read `sessionMgr.LastTurnMeta()`.
- **Preserve all behavior**: turn-row shape, response-ID fallback chain, per-turn token usage, secret redaction before persist, replay reasoning-strip (cache-stable), bounded replay, and the `is_summary` flag.

## Capabilities

### Modified Capabilities

- `conversation-history`: history load and turn persist SHALL be owned by a business-layer `SessionManager`; the runner SHALL receive the assembled message list and SHALL NOT load or persist history itself. All existing behavior requirements (turn-row shape, response-ID non-empty, redaction, bounded replay, reasoning-strip, `is_summary`) are preserved.
- `agent-core`: `Agent.Run` SHALL accept the assembled message list (`[]*schema.AgenticMessage`); a framework `TurnCollector` SHALL accumulate the turn from agent state and hand it to an injected committer at terminal without owning storage.

## Impact

**Affected code:**
- `internal/conversation/` — NEW package: `session_manager.go`, `persistence_helpers.go`, `doc.go`, black-box `*_test.go`.
- `internal/agent/agent.go` — `Run` signature; `AssembleAgentOpts` (`ConvStore`/`ConversationID` → `Committer`); new `TurnCommitter` interface; remove `Agent.LastTurnMeta`; keep `Agent.SetEventSink`.
- `internal/agent/middlewares/history_middleware.go` — replaced by a framework `TurnCollector` (accumulate + usage emit + committer call); delete the persistence helpers that move to `internal/conversation`.
- `internal/cli/agent_session.go`, `internal/cli/chat.go`, `internal/cli/run.go` — build `SessionManager`, `LoadHistory`, assemble, `Run(messages)`, `sessionMgr.LastTurnMeta()`.
- `internal/api/service`, `internal/api/handler/chat.go` — service returns `*conversation.SessionManager`; handler loads history, assembles, runs, reads meta from the session manager. SSE streaming loop unchanged.

**Affected systems:** agent runtime, conversation store consumption, API SSE, CLI REPL/run.

**Dependencies:** none new. The store layer (`internal/store`, `internal/store/sqlite/conversation.go`) is unchanged.

**Non-goals:** abnormal-terminal (error/interrupt/cancel) persistence and terminal-reason surfacing — those remain the separate `persist-turn-and-surface-lifecycle` change, which will hook the iterator's abnormal terminals to the same `SessionManager.CommitTurn` seam introduced here. Wiring the currently-unwired `SaveSummary` (durable compaction cursor). Adopting the `adk.NewTypedRunner` wrapper. Changing memory flush, skills, hooks, filesystem, or summarization middleware.