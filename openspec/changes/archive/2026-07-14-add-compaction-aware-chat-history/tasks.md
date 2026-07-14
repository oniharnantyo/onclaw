## 1. Store: flag summary rows

- [x] 1.1 Add `is_summary INTEGER NOT NULL DEFAULT 0` to `conversation_messages` via a
  migration in `internal/store/sqlite/db.go`.
- [x] 1.2 In `SaveSummary` (`internal/store/sqlite/conversation.go`), set `is_summary = 1`
  on insert.
- [x] 1.3 Add `IsSummary bool` to `TurnRow` (`internal/store/types.go`); include the column
  in the `ListTurns` SELECT.
- [x] 1.4 Backfill existing summary rows via `summary_message_id`.

## 2. Store: compaction metadata

- [x] 2.1 Add a query/derivation for conversation-level `compaction_count` (count of
  `is_summary` rows) and `last_compaction_at` (latest summary row `created_at`).

## 3. API: expose flag + metadata

- [x] 3.1 The `ListMessages` response (`internal/api/{service,handler}/conversation.go`)
  SHALL include per-turn `is_summary` and conversation-level `compaction_count` /
  `last_compaction_at`.

## 4. Agent: summarization middleware config

- [x] 4.1 In `internal/agent/agent.go`, provide a `TokenCounter` whose baseline is the most
  recent assistant message's `PromptTokens` (mirror Eino's default anchor-and-increment;
  swap the anchor field from `TotalTokens`).
- [x] 4.2 Set `Trigger.ContextMessages` backstop (default 200).
- [x] 4.3 Enable `Retry` with bounded `MaxRetries` (default 2).
- [x] 4.4 Set `TranscriptFilePath` to a per-conversation transcript of the compacted range
  (export rows with `sequence_num <= summary_until_seq`).

## 5. Web: compaction boundary marker

- [x] 5.1 Add `is_summary` to the chat message type (`web/src/types/chat.ts`); tag summary
  turns in `ChatProvider.fetchMessages`.
- [x] 5.2 Render summary turns as a marker (divider + collapsible summary) in
  `Renderers.tsx`, reusing the existing collapsible idiom; keep them visible (do not drop in
  `isMessageVisible`).
- [x] 5.3 Confirm non-summary messages remain fully visible.

## 6. Web: context meter annotation

- [x] 6.1 Track previous `compaction_count`; when it increases after the post-turn re-fetch,
  show a one-time "context compacted" annotation on the meter.
- [x] 6.2 Guard meter source selection: do not anchor `used` on a summary row.

## 7. Testing (black-box, ≥ 70% coverage per package)

- [x] 7.1 Store: summary row returns `IsSummary=true`; normal turns `false`; re-compaction
  yields multiple flagged rows; metadata count/timestamp correct.
- [x] 7.2 API: response includes `is_summary`, `compaction_count`, `last_compaction_at`.
- [x] 7.3 Agent: custom `TokenCounter` anchors on `PromptTokens` (a long completion does not
  inflate the measured count); `Retry` / `ContextMessages` / `TranscriptFilePath` wired.
- [x] 7.4 Web: `groupBlocks` / renderer test — summary turn renders as a marker, not a
  bubble; meter annotation fires on count increase.

## 8. Verification

- [x] 8.1 `make vet && make test` clean; web test runner green.
- [ ] 8.2 Manual: run a conversation past the threshold; confirm the marker appears at the
  boundary, old messages remain visible, the meter annotates the drop, and the agent can
  read the compacted transcript via the cited path.

## 9. Spec

- [x] 9.1 Spec deltas under `specs/conversation-history/spec.md` and `specs/chat-ui/spec.md`.
  (This proposal.)

## 10. Input-safety floor guard

- [x] 10.1 Add `estimateFloorTokens(ctx, instruction, tools)` in `internal/agent/input_safety.go`
  (reuses `estimateTokenCount`; tool schemas marshaled with `Extra = nil`).
- [x] 10.2 Add threshold + sentinel in `internal/agent/middlewares/input_safety.go`
  (`FloorSafetyFraction = 0.5`, `FloorSafetyLimit(window)`, `ErrInputFloorExceedsSafetyLimit`).
- [x] 10.3 Build-time guard in `AssembleAgent` (`internal/agent/agent.go`) after tools are
  finalized: fail fast when `floor >= FloorSafetyLimit(contextWindow)`.
- [x] 10.4 `InputSafetyMiddleware` (`internal/agent/middlewares/input_safety_middleware.go`)
  overriding `BeforeModelRewriteState`: re-estimate the tool floor from the live
  `state.ToolInfos` each turn (plus precomputed system-prompt tokens) and fail fast when it
  reaches the limit.
- [x] 10.5 Register the preflight at handlers index 0 (before `summarizationMiddleware`).
- [x] 10.6 Map `ErrInputFloorExceedsSafetyLimit` to HTTP 400 in
  `internal/api/handler/chat.go`.
- [x] 10.7 Black-box tests (>= 70% per package); manual repro of the 5000/7400 case.
