## Context

- `TurnCollector` is the only caller of `Committer.CommitTurn` (`turn_collector.go:87`). It accumulates the turn from framework `state.Messages` (`accumulateNewMessages`, skipping system + persisted) and at `AfterAgent` hands it to `SessionManager.CommitTurn`. It also emits per-call usage to the `EventSink` (SSE context meter) from `ResponseMeta`.
- Verified against Eino `v0.10.0-alpha.12`: `ResponseMeta` (response-ID + `TokenUsage`) and the complete turn (incl. tool-result messages) are assembled **only** in `state.Messages`. The ReAct loop appends tool results to state via the tools node (`adk/react.go:499,744`) as internal graph edges. There is **no** outside-state capture mechanism — `model.WithCallback` does not exist, and there is no top-level `callback` bus in this version (path absent; onclaw does not use it).
- Therefore "remove the collector entirely" forces the business layer to reconstruct the turn from the **event stream** and to **estimate** tokens, accepting the loss of provider-reported usage and provider-extension response-IDs.

## Goals / Non-Goals

**Goals**
- No persistence middleware in the agent runner; `TurnCollector` and `Committer` deleted.
- Entrypoint reconstructs the turn from the event stream and drives `SessionManager.CommitTurn`.
- Token usage estimated on the business layer (char-based).

**Non-Goals**
- Dropping the ADK agent (only path to zero framework state access).
- Provider-accurate token usage or provider-extension response-IDs (accepted losses).
- `SaveSummary`, Runner adoption, PromptTokens TokenCounter, persist-turn change.

## Decisions

### 1. Delete the collector; entrypoint owns the commit trigger
`TurnCollector` and the `Committer`/`TurnCommitter` interface are removed. The entrypoint calls `sessionMgr.CommitTurn(collectedTurn)` after its drain loop. Persistence leaves the agent runner entirely.

### 2. Reconstruct the turn from the event stream (no state access)
The event iterator collects every turn message into a buffer exposed to the entrypoint: capture all `MessageOutput.Message` events (today only non-streaming ones hit `collectedMsgs`, `event_iterator.go:92`) and **reassemble streamed assistant messages** from their chunk stream (today chunks are only forwarded, `event_iterator.go:40`). The business layer never touches `state.Messages`.

### 3. Char-based token estimation on the business layer
`SessionManager.CommitTurn` replaces the `ResponseMeta.TokenUsage` read with `estimateTokenCount` (chars/4, `internal/agent/summarization_config.go:11`) over the turn's messages. The estimator is owned by the business layer (move/copy into `internal/conversation`).

### 4. Response-ID via `_eino_msg_id`
Provider-extension IDs (OpenAI/Gemini, state-only) are no longer reachable. The fallback reads `_eino_msg_id` from the final assistant message's `Extra` (which travels on the streamed message); else empty.

### 5. Tool-result persistence is spike-gated (see Risks)
Whether tool-result messages surface as consumer-facing `MessageOutput` events is unverified. A blocking spike confirms it before the rest of the work proceeds.

## Risks / Trade-offs

- **Token accuracy:** char-based estimate replaces provider-reported usage. Accepted.
- **Response-ID relaxation:** provider-extension IDs dropped; rely on `_eino_msg_id` or empty. Tensions with the existing `conversation-history` requirement "Response ID must never be empty" — that requirement is MODIFIED here to allow the relaxed fallback.
- **Tool-result persistence (the material risk):** the persisted turn contains tool results **only if** the ADK event stream emits them as consumer events. If it does not, stored conversations lose tool-result content, conflicting with the requirement that a tool-calling turn's array contains tool-result messages. **Mitigation:** a blocking spike (Implementation Task 1) asserts a tool-calling turn's collected buffer contains a `FunctionToolResult` block. If it fails, escalate the decision: (a) accept the loss + update the spec, or (b) retain a minimal state-reader for tool results only (which contradicts "remove entirely" and would re-open this design).
- **Streamed-assistant reassembly correctness:** reassembling chunks into the complete message (with tool-call blocks) must be exact; covered by a dedicated test.