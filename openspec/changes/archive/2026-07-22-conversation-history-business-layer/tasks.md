# Implementation Tasks

## 1. Business-layer package `internal/conversation/`

- [x] 1.1 Create `internal/conversation/doc.go` (package doc: business-layer owner of conversation history per 03.md).
- [x] 1.2 `internal/conversation/persistence_helpers.go`: move `unmarshalTurn` (mark `_onclaw_persisted`+`_onclaw_seq`), `stripReplayReasoning`, `SanitizeSummaryMessage`, `IsPersisted`, `getAgenticMessageText`, `extractQuestionAndAnswer`, `persistedKey`, `uuidRegex` from `history_middleware.go` (verbatim logic).
- [x] 1.3 `internal/conversation/session_manager.go`: `SessionManager{store, conversationID, model, previousResponseID, lastTurnMeta, lock}`; `NewSessionManager`.
- [x] 1.4 `LoadHistory(ctx)`: `store.LoadHistory` → per-turn unmarshal+mark → strip reasoning → sanitize summary → derive previousResponseID (last tail, else summary, else `middlewares.GetPreviousResponseID` client override). Returns `([]*schema.AgenticMessage, string, error)`.
- [x] 1.5 `CommitTurn(ctx, messages)`: extract question/answer; response-ID fallback (OpenAIExt→GeminiExt→`_eino_msg_id` UUID-validated→empty+warn); tokens from final assistant `ResponseMeta.TokenUsage`; `tools.StripReasoning(tools.RedactAgenticMessage(msg))` per msg; mark persisted; `json.Marshal`; `store.AppendTurn` under `context.WithoutCancel(ctx)`+5s; set `lastTurnMeta`; empty-buffer + persisted-flag idempotency.
- [x] 1.6 `LastTurnMeta()`, `SetPreviousResponseID(string)`.
- [x] 1.7 `internal/conversation/*_test.go` (black-box `package conversation_test`, ≥70%): LoadHistory (empty/tail/summary/reasoning-strip-byte-stable/prevID), CommitTurn (basic/redacts-secrets/strips-reasoning/response-ID-fallback-chain/tokens/idempotent/WithoutCancel), helpers.

## 2. Framework `TurnCollector` (replaces `HistoryMiddleware`)

- [x] 2.1 `internal/agent/middlewares/turn_collector.go`: embeds `adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]`; holds `Committer` (interface from `internal/agent`) + `EventSink` + `bufferedMessages` + `lock`.
- [x] 2.2 `AfterModelRewriteState`: `accumulateNewMessages(state.Messages)` (skip system+persisted, dedupe by pointer) **+ emit usage via `EventSink`** (move `:163-176`).
- [x] 2.3 `AfterAgent`: final `accumulateNewMessages`, then `Committer.CommitTurn(ctx, bufferedMessages)`; clear buffer.
- [x] 2.4 `SetEventSink`; no `BeforeAgent`, no store, no write, no `LastTurnMeta`.
- [x] 2.5 Keep `WithPreviousResponseID`/`GetPreviousResponseID` ctx helpers in `middlewares` (used by API).
- [x] 2.6 Tests (`turn_collector_test.go`): accumulates only new (skips system+persisted), emits usage, hands turn to a fake committer, fires on `AfterAgent`.

## 3. `Agent` + assembly (`internal/agent/agent.go`)

- [x] 3.1 Add `type TurnCommitter interface { CommitTurn(context.Context, []*schema.AgenticMessage) (*store.TurnMeta, error) }`.
- [x] 3.2 `AssembleAgentOpts`: add `Committer`; retain `ConvStore` (transcript loader) and `ConversationID` (transcript/hooks/tool-session scoping).
- [x] 3.3 `buildMiddleware`: construct `TurnCollector{Committer: opts.Committer}` instead of `NewHistoryMiddleware` (`:387`); update chain entry (`:410`).
- [x] 3.4 `Run(ctx, messages []*schema.AgenticMessage) EventIterator` (`:476`): drop `userInput`/`contentBlocks` and the `UserAgenticMessage` build (`:499-504`); set `input.Messages = messages`. Keep `onTurnError`/`onStopFlush` wiring.
- [x] 3.5 Remove `Agent.LastTurnMeta` (`:541`); keep `Agent.SetEventSink` delegating to the collector; replace `historyMiddleware` field (`:45`) with `turnCollector`.

## 4. Entrypoint migration

- [x] 4.1 `internal/cli/agent_session.go`: `resolveAndAssemble` builds `conversation.NewSessionManager(convStore, convID, agentConf.Model)` and passes as `Committer`; drop `convStore`/`convID` params to `AssembleAgent`.
- [x] 4.2 `internal/cli/chat.go`: per agent, `sessionMgr.LoadHistory` → assemble `history + userMsg` → `Run(messages)` → drain (unchanged render) → `sessionMgr.LastTurnMeta()`.
- [x] 4.3 `internal/cli/run.go`: same shape (one conversation per invocation).
- [x] 4.4 `internal/api/service`: `Chat` returns `(convID, *agent.Agent, *conversation.SessionManager, error)`.
- [x] 4.5 `internal/api/handler/chat.go`: `SetEventSink` unchanged; `LoadHistory` → assemble → `Run(messages)`; drain loop unchanged; `sessionMgr.LastTurnMeta()` for the `turn` SSE event.

## 5. Cleanup

- [x] 5.1 Delete `internal/agent/middlewares/history_middleware.go`, `history_middleware_test.go`, `history_middleware_export_test.go`.
- [x] 5.2 Update any remaining references (exports, mocks) in `internal/agent/*_test.go` to the new `TurnCollector`/`AssembleAgentOpts`.
- [x] 5.3 Confirm `internal/store` and `internal/store/sqlite/conversation.go` are untouched.

## 6. Verification

- [x] 6.1 `make fmt && make vet && make lint`.
- [x] 6.2 `make test` green; `internal/conversation`, `internal/agent`, `internal/api/handler`, `internal/cli` each ≥70%.
- [x] 6.3 Integration: one completed turn writes exactly one `conversation_messages` row (correct messages/model/tokens/response_id/question/answer); multi-turn REPL reuses one conversation and replays bounded history; redaction applied before persist.
- [x] 6.4 Manual: `onclaw chat` multi-turn, `onclaw run` one-shot, web UI chat (SSE streams, context meter updates, `turn` event carries metadata) — behavior unchanged.
- [x] 6.5 `openspec validate conversation-history-business-layer`.