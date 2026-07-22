## 1. Cluster 1 — post-compaction crash (showstopper, land first)

- [x] 1.1 `internal/agent/agent.go` `handleSummarization`: append the recency
  note as `user_input_text` (not `assistant_gen_text`) so the `role: user`
  summary stays role-valid for provider conversion.
- [x] 1.2 Unit test: after `handleSummarization`, every block on the summary
  message is a user-valid type.
- [x] 1.3 Replay/convert test: a summary message round-trips through the eino
  converter without `NodeRunError`.

## 2. Cluster 2 — extraction-when-disabled + embedder 400

- [x] 2.1 `internal/agent/agent.go`: add `ExtractionEnabled` to
  `handleSummarizationParams`; gate the compaction-time `ExtractAndFlush` on it
  (pass `resolvedMem.ExtractionEnabled` from the callback; `resolvedMem` is in
  scope from `:104`).
- [x] 2.2 `internal/memory/embedding.go`: guard `Embed` to return `nil, nil`
  when `ModelName == ""` (no provider call, no 400) — extend the existing
  `Provider == nil` guard.
- [x] 2.3 Tests: `ExtractAndFlush` not called when `ExtractionEnabled=false`
  (fake `MemoryStore`/`Embedder` counting calls); `Embed` returns nil/nil with
  an empty model and makes no provider call.

## 3. Cluster 4 — summary `answer` extraction + FTS

- [x] 3.1 `internal/store/sqlite/conversation.go` `SaveSummary`: read
  `user_input_text` blocks into `answer` too (mirror `agent.go:620-638`); keep
  the `assistant_gen_text` branch for legacy shapes. FTS auto-syncs via the
  existing `conversation_messages_ai` trigger — no FTS code change.
- [x] 3.2 Rewrite the currently-broken half-written test cleanly as
  `TestConversationStoreSummaryExtractsUserInputText`: `SaveSummary` with a
  row-27-shaped message → `ListTurns` → assert `Answer` contains the real summary
  + an FTS `MATCH` assertion proving searchability. Use `*store.TurnRow`,
  black-box `sqlite_test`, existing `setupTestDB`.

## 4. Capstone — `SummaryMessage` contract

- [x] 4.1 Add `validateSummaryMessage(msg) error` (role == user; only
  `user_input_text` blocks).
- [x] 4.2 Call it in `handleSummarization` before `SaveSummary` (reject invalid).
- [x] 4.3 Call it in `HistoryMiddleware.BeforeAgent` after loading the summary
  row, **sanitizing** (stripping role-invalid blocks) before replay — graceful
  degradation for legacy rows, no backfill.
- [x] 4.4 Tests: invalid shape rejected at persist; a legacy
  `assistant_gen_text`-bearing summary row is sanitized (not crashed) on replay.

## 5. Cluster 3 — progressive meter + live compaction indicator

- [x] 5.1 `internal/agent/event.go` (new): `EventSink` interface
  (`EmitCompactionStart`, `EmitCompactionEnd`, `EmitUsage`) + no-op default.
- [x] 5.2 `internal/agent/agent.go`: add `sink EventSink` + `SetEventSink`; wire
  the summarization callback to emit compaction start/end (forward-reference
  pattern as used for `memMW`).
- [x] 5.3 `internal/agent/middlewares/history_middleware.go`: emit per-step
  `usage` in `AfterModelRewriteState`.
- [x] 5.4 `internal/api/handler/chat.go`: after `NewSSEWriter`, before `Run`,
  `assembledAgent.SetEventSink(...)` writing `compaction` and `usage` SSE events.
- [x] 5.5 Frontend: `types/chat.ts` (new event types), `runChatStream.ts`
  (`onCompaction`/`onUsage`), `ChatProvider.tsx` (`isCompacting` +
  `onUsage`→`SET_CONTEXT_USED`; replace `prompt ?? total` with
  `prompt > 0 ? prompt : total`), `Chat.tsx` ("Compacting context…" indicator).
- [x] 5.6 Verify `ResponseMeta.TokenUsage` is available at
  `AfterModelRewriteState` (fallback: model `OnEnd` / streamed final chunk).

## 6. Verification

- [x] 6.1 `gofmt -w .` + `go vet ./internal/...`.
- [x] 6.2 `go test ./internal/agent/... ./internal/api/... ./internal/store/sqlite/...`
  green; coverage ≥ 70%.
- [x] 6.3 `cd web && npx tsc --noEmit` + frontend tests green.
- [x] 6.4 Manual: drive a conversation past the first compaction — the turn
  completes (no `NodeRunError`); with memory disabled, compaction does no
  extraction and no embedder 400; the "Compacting context…" indicator shows;
  the meter ticks per model step.
