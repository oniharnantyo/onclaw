# Tasks: Refactor conversation_messages to turn-per-row

## Store contract & types
- [x] Add `TurnRow` (id, conversation_id, sequence_num, response_id, previous_response_id, message, model, prompt/completion/total tokens, question, answer, created_at); retire `MessageRow`.
- [x] Rewrite `ConversationStore`: `AppendTurn(ctx, convID, msgArrayJSON, responseID, previousResponseID, model, prompt, completion, total, question, answer) (seq, error)`; `LoadHistory` → `(summary *TurnRow, tail []*TurnRow, err)`; `ListTurns`; adapt `SaveSummary` (turn granularity); `ListConversations` (join first turn's `question`).

## SQLite schema & migration
- [x] Reshape `conversation_messages` to turn-per-row (drop `role`; add `sequence_num`, `response_id`, `previous_response_id`, `model`, token columns, `question`, `answer`); index `(conversation_id, sequence_num DESC)` and `(response_id)`.
- [x] Move FTS to `question || ' ' || answer`; recreate sync triggers; drop the old `message`-FTS backfill.
- [x] Guarded clean rebuild: if old shape (`role` column present), `DROP` table + FTS, recreate. One-time wipe of pre-release data.

## History middleware (buffered turn commit)
- [x] `BeforeAgent`: `LoadHistory` → unmarshal + concatenate turn arrays → prepend to `AgentInput.Messages`. Flag loaded messages `_onclaw_persisted`; buffer the new user message (no eager write).
- [x] `AfterModelRewriteState` / `AfterAgent`: accumulate new assistant/tool messages; at `AfterAgent` commit one turn row via `AppendTurn`, then flag buffered messages persisted.
- [x] Extract `question`/`answer` (role-based parsing); read token usage + `response_id` from the final assistant message; set `previous_response_id` from the last loaded turn or client override.
- [x] Record `lastTurnMeta` on the middleware; expose a getter.

## Bounded replay
- [x] `LoadHistory`: no summary → all turns; summary present → summary + last 3 turns past the coverage cursor (`tailTurnWindow = 3`).
- [x] Confirm the summarization trigger (existing `summarization` middleware, 80% context window) compacts turns older than the 3-turn window and advances `summary_until_seq`.

## Search
- [x] `session_search`: match `question`/`answer`; per-turn result format; drop single-`AgenticMessage` unmarshal.

## Chat API
- [x] `ChatInput.PreviousResponseID` (optional request field) in `internal/api/service/types.go`.
- [x] Terminal `turn` SSE event `{conversation_id, sequence_num, response_id, previous_response_id, model, tokens}` in `internal/api/handler/chat.go`.
- [x] `Agent` retains `*HistoryMiddleware` ref + `LastTurnMeta()` accessor (`internal/agent/agent.go`); handler reads it after the iterator drains.

## Web API + UI
- [x] `ListMessages` → returns turns; DTO exposes `sequence_num`, `message[]`, `model`, tokens, `question`, `answer`.
- [x] Web chat renderer flattens `turn.message[]`; conversation-list preview uses the first turn's `question` (updating `getConversationTitle` in Chat.tsx).

## Testing (black-box; ≥70% per package)
- [x] One multi-message turn → one row, 4-element array, correct question/answer, incrementing sequence_num.
- [x] Replay fidelity: `LoadHistory` concatenation reproduces the exact input array.
- [x] Bounded tail: ≥5 turns + summary → `LoadHistory` returns summary + exactly last 3.
- [x] Threading: turn 2's `previous_response_id` == turn 1's `response_id`; turn 1's == "".
- [x] History-to-agent: second run on same conversation prepends prior turn(s).
- [x] Chat API: `turn` SSE event emitted; `PreviousResponseID` accepted.
- [x] `make vet && make fmt && make test` green.

## Deferred (separate changes)
- [ ] Stateful-provider Path B threading (send `previous_response_id` + delta).
- [ ] Real-adapter wiring for token usage + response_id population.