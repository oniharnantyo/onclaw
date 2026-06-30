# Implementation Tasks

## 1. Store contract & types

- [x] 1.1 Add `Conversation` and `MessageRow` DTOs to `internal/store/types.go` (store stays eino-free; `Message` field is an opaque JSON string)
- [x] 1.2 Add the `ConversationStore` interface to `internal/store/store.go` (`CreateConversation`, `AppendMessage` returning assigned `seq`, `LoadHistory`, `ListMessages`, `SaveSummary`)

## 2. SQLite schema & migration

- [x] 2.1 Add `conversations` and `conversation_messages` tables (with `idx_conversations_agent`, `idx_messages_conv_seq`, `idx_messages_role`) to `Migrate()` in `internal/store/sqlite/db.go` using `CREATE TABLE IF NOT EXISTS`
- [x] 2.2 Add guarded `ALTER` columns `summary_until_seq` (default 0) and `summary_message_id` (default 0) to `conversations` via the existing `columnExists` helper
- [x] 2.3 Implement atomic per-conversation `seq` assignment in the insert statement (`COALESCE(MAX(seq),0)+1` scoped to `conversation_id`)

## 3. ConversationStore SQLite implementation

- [x] 3.1 Create `internal/store/sqlite/conversation.go` implementing `store.ConversationStore` (mirror `sqliteXStore{db}` / `NewConversationStore` pattern from `kv.go`/`profile.go`)
- [x] 3.2 Implement `LoadHistory`: return the summary row (when `summary_message_id != 0`) plus live tail = rows with `seq > summary_until_seq` AND `id <> summary_message_id`, ordered by `seq`
- [x] 3.3 Implement `SaveSummary`: insert the summary as a normal message row, then set `conversations.summary_message_id` and `summary_until_seq = coveredUntilSeq` + `updated_at`
- [x] 3.4 Create `internal/store/sqlite/conversation_test.go` (reuse `testutil.go`): create→append user/assistant/tool→`LoadHistory`→`ListMessages` round-trip; `SaveSummary` advances cursor and `LoadHistory` then returns `[summary]+tail` excluding compacted rows


## 4. HistoryMiddleware

- [x] 4.1 Create `internal/agent/history.go`: a `ChatModelAgentMiddleware` embedding `adk.TypedBaseChatModelAgentMiddleware[*schema.Message]`, holding `store.ConversationStore` + `conversationID`
- [x] 4.2 Implement `BeforeAgent`: `LoadHistory`, inject `[summary]+tail` before the new user message in `runCtx.AgentInput.Messages`, mark injected messages persisted, save+mark the new user message immediately, init per-run cursor (carried via context)
- [x] 4.3 Implement the eager-save scan: marshal + `tools.Redact` + `AppendMessage` for each unmarked message, then mark it (persisted-marker in `Extra`); track `maxSeq`
- [x] 4.4 Wire the scan into `AfterModelRewriteState` (save after each model response) and `AfterAgent` (final flush for trailing tool results)
- [x] 4.5 Unit test the marker/compaction behavior: simulate a compacted state slice and assert new messages save exactly once (no loss, no duplicates)


## 5. Durable compaction coordination

- [x] 5.1 Resolve the open question: write a focused test confirming whether eino summarization preserves message object identity/`Extra` on preserved messages (informs duplicate-row risk); add `LoadHistory` content-hash dedup if it does not
- [x] 5.2 Add a summarization `Config.Callback` that, after each compaction, persists the summary row + sets `summary_until_seq` to the current `maxSeq` via `SaveSummary`

## 6. Agent assembly wiring

- [x] 6.1 Update `AssembleAgent` (`internal/agent/agent.go`) to accept `convStore store.ConversationStore` + `conversationID int64`; construct the `HistoryMiddleware` and the summarization `Callback`, add both to `Handlers` (order: summarization with Callback, then history)
- [x] 6.2 Remove the `transcriptPath` computation and `TranscriptFilePath` from the summarization config; drop now-unused imports

## 7. CLI conversation lifecycle

- [x] 7.1 Construct `sqlite.NewConversationStore(db)` in `getProviderManager` (`internal/cli/context.go`) and expose it to commands alongside the other stores
- [x] 7.2 `internal/cli/run.go`: create one conversation per invocation (`CreateConversation`), pass `convStore` + `convID` into `AssembleAgent`
- [x] 7.3 `internal/cli/chat.go`: create one conversation at REPL start and reuse its `convID` across turns

## 8. Runner simplification & transcript removal

- [x] 8.1 Remove the `transcriptPath` parameter and all `AppendToTranscript` call sites from `RunAgent` (`internal/agent/runner.go`); runner becomes streaming-only (`RunAgent(ctx, a, userInput, stdout io.Writer) error`)
- [x] 8.2 Delete `internal/agent/transcript.go` (only `runner.go` referenced it); confirm `tools.Redact` remains in `internal/agent/tools`


## 9. Verification

- [x] 9.1 `make vet && make test` green, including new store and middleware tests
- [x] 9.2 Round-trip fidelity test: marshal a `*schema.Message` (content + ToolCalls + `Extra` with summarization tag + `_onclaw_persisted` marker), store, reload, unmarshal → assert equality
- [x] 9.3 Manual: `make build && make run ARGS='chat'` — turn 2 refers to turn 1 and the agent remembers; inspect `onclaw.db` (one `conversations` row, monotonic `seq`, no `.jsonl` written)
- [x] 9.4 Manual: drive a conversation past the ~6000-token threshold → summary row appears, `summary_until_seq` advances, next turn replays `[summary]+tail` (bounded)
- [x] 9.5 `grep -rn AppendToTranscript internal/` returns empty; `go build ./...` clean