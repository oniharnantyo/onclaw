## Why

The `TurnCollector` middleware (`internal/agent/middlewares/turn_collector.go`) is the only caller of `Committer.CommitTurn` (`turn_collector.go:87`) — it owns the persistence trigger from inside the agent's middleware chain. The prior `conversation-history-business-layer` change moved the *write logic* to `SessionManager` but left the collector as the in-runner trigger that reads framework `state.Messages` and calls `CommitTurn`. The user wants persistence **entirely out of the agent runner**: no persistence middleware, with token usage **estimated on the business layer** (char-based) rather than read from framework `ResponseMeta`.

Investigation against Eino `v0.10.0-alpha.12` confirms the constraint: the final assistant's `ResponseMeta` (provider response-ID + `TokenUsage`) and the complete turn (including tool-result messages) are assembled **only** in framework `state.Messages` — the ReAct loop appends tool results to state via the graph's tools node (`adk/react.go:499,744`) as internal graph edges, not confirmed consumer-facing stream events. There is no outside-state mechanism in this version (no `model.WithCallback`; no top-level `callback` bus — path absent). Therefore removing the state-reader means the business layer works from the **event stream** plus its own char-based estimation.

## What Changes

- **Delete `TurnCollector` and the `Committer`/`TurnCommitter` interface entirely** — no persistence middleware remains in the agent runner.
- **Reconstruct the turn from the event stream in the entrypoint**: the event iterator collects every turn message it sees (all `MessageOutput.Message` events, reassembling streamed assistant chunks) and exposes it; the entrypoint calls `sessionMgr.CommitTurn(collectedTurn)` after draining.
- **Estimate token usage on the business layer**: `SessionManager.CommitTurn` computes prompt/completion/total via the char-based `estimateTokenCount` heuristic over the turn's messages, instead of reading `ResponseMeta.TokenUsage`.
- **Response-ID via `_eino_msg_id`**: read from the final assistant message's `Extra` (stream-carried) where present, else empty — the provider-extension IDs (state-only) are no longer available.
- **Usage meter** (SSE context meter) is driven by the business-layer estimate rather than the collector's `ResponseMeta` read.

## Capabilities

### Modified Capabilities

- `conversation-history`: turn persistence is driven by the entrypoint from the event stream (no framework persistence middleware); per-turn token usage is a business-layer char-based estimate; response-ID falls back to `_eino_msg_id` or empty.

## Impact

**Affected code:**
- `internal/agent/middlewares/turn_collector.go` (+ `_test.go`) — **deleted**.
- `internal/agent/agent.go` — remove `TurnCommitter` alias (`:64`), `AssembleAgentOpts.Committer` (`:79`), collector wiring (`:391/:414/:45/:116/:467`), `SetEventSink` delegation (`:545`).
- `internal/agent/event_iterator.go` + `iterator.go` — collect all turn messages + reassemble streamed assistant; expose `CollectedTurn()`.
- `internal/conversation/session_manager.go` — char-based token estimation; `_eino_msg_id` response-ID fallback.
- `internal/cli/chat.go`, `internal/cli/run.go`, `internal/cli/agent_session.go`, `internal/api/handler/chat.go`, `internal/api/service` — drop `Committer` plumbing; post-drain `CommitTurn(collectedTurn)`.

**Affected systems:** agent runtime (middleware chain), event iteration, conversation persistence, API SSE meter, CLI.

**Dependencies:** none new.

**Non-goals:** dropping the ADK agent to own the model loop (the only path to *zero* framework state access — large rewrite). Wiring `SaveSummary`. Adopting `adk.NewTypedRunner`. The PromptTokens `TokenCounter` (`conversation-history:174`). The separate `persist-turn-and-surface-lifecycle` change (abnormal-terminal persistence becomes natural under entrypoint-driven commit, but that change stays separate).