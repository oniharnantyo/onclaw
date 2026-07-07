# Proposal: Refactor conversation_messages to turn-per-row

## Intent
Reshape `conversation_messages` from **one row per message** to **one row per turn** (Reading A — "turn delta"): each row stores the turn's message array as a JSON delta (the new messages produced in that turn, ending at the final assistant response), plus the model used, per-turn token usage, denormalized `question`/`answer` text, and `response_id`/`previous_response_id` for follow-up threading. Full history is reconstructed by concatenating turn arrays in `sequence_num` order; the live send to the model is **all turns until the context threshold, then summary + last 3 turns**.

## Problem
Today (`internal/store/sqlite/db.go`, `internal/store/sqlite/conversation.go`) every `AgenticMessage` is its own row, persisted eagerly per-message by `HistoryMiddleware`. This leaves no place to record:
- **Per-turn token usage** or the **model** that produced a turn (usage is never captured — every `Usage` reference in the repo is a CLI help string).
- A cheap **conversation-list preview** (`ListConversations` runs a correlated subquery to dig a preview out of JSON) or **clean FTS** (`session_search` indexes the noisy `message` JSON blob and unmarshals a single `AgenticMessage` per row).
- A **response-id chain** for follow-up turns on stateful providers (OpenAI Responses / OpenResponses).

## Proposed Solution
**Row shape** — turn-per-row: `sequence_num`, `response_id`, `previous_response_id`, `message` (JSON array of the turn's `AgenticMessage` deltas), `model`, `prompt_tokens`/`completion_tokens`/`total_tokens`, `question`, `answer`. Drop `role` (a turn mixes roles; role lives inside each array element).

**Replay/send policy** (Path A — reconstruct + send, bounded by compaction): `HistoryMiddleware.BeforeAgent` concatenates the turn arrays and prepends to `runCtx.AgentInput.Messages`. Pre-threshold it sends **all turns**; once the reconstructed list nears the context threshold (the existing `summarization` trigger at 80% of the window), compaction fires and subsequent loads send **summary + last 3 turns** (`LoadHistory` returns the summary + the last 3 turns past the coverage cursor; `tailTurnWindow = 3`).

**Turn commit** — `HistoryMiddleware` accumulates the turn's new messages in an in-memory buffer and commits **one** row at `AfterAgent` (the existing turn-end hook), extracting `question` (first user block) and `answer` (last assistant block) via the existing `getAgenticMessageText`, and `response_id` + token usage from the final assistant message's response metadata.

**Chat API** — `POST /api/chat` gains an optional `previous_response_id` request field and emits a terminal `turn` SSE event carrying `{conversation_id, sequence_num, response_id, previous_response_id, model, tokens}` so the client can chain follow-ups.

**Search** — `session_search` moves FTS to `question`/`answer` text; results render per-turn.

## Constraints & Dependencies
- **On-device footprint:** O(n) storage (Reading A). The O(n²) full-snapshot reading is rejected — ~8 GB SBC target.
- **Store/agent separation:** the store package stays free of eino imports; the middleware marshals the message array and passes extracted fields to `AppendTurn`.
- **Eino reuse:** turn boundary = existing `AfterAgent` hook; summarization = existing `summarization` middleware + `handleSummarization` + `SaveSummary`; only the unit (turn) and the tail window (3) are new.
- **Token/usage capture** reads `ResponseMeta.TokenUsage`; the only registered adapter is the **stub** (`internal/llm/adapter/stub.go`), so counts (and `response_id`) are 0/empty until a real adapter lands — schema and plumbing are forward-compatible.
- **Live send is reconstructed history**, not response_id threading. `response_id`/`previous_response_id` are stored + surfaced for audit and future stateful-provider optimization; they are not used in the live reconstruction path.

## Out of Scope (Deferred)
- **Stateful-provider threading (Path B):** sending `previous_response_id` + delta instead of reconstructed history. The columns make it possible; wiring it is a separate change once a stateful adapter exists.
- **Migration of old data:** pre-release; existing one-message-per-row rows are wiped (clean rebuild, one-time guarded reshape).
- **Partial-turn crash recovery:** a turn that crashes before `AfterAgent` leaves no row (matches "turn = complete exchange ending in a response").