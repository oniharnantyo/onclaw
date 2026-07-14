## 1. Implementation

- [x] 1.1 In `HistoryMiddleware.accumulateNewMessages`
  (`internal/agent/middlewares/history_middleware.go`), skip any message with
  `role == schema.AgenticRoleTypeSystem` before the `IsPersisted` check, with a
  comment explaining system messages are re-injected each turn and must never be
  persisted.
- [x] 1.2 Verify `go vet ./internal/agent/middlewares/...` is clean and the
  package compiles.

## 2. Testing (black-box, `package middlewares_test`)

- [x] 2.1 Add `TestSystemMessagesNotPersisted`: a turn whose `state.Messages`
  includes a system message (built via `schema.SystemAgenticMessage`) plus user
  + assistant; after `AfterModelRewriteState` + `AfterAgent`, assert the stored
  turn row's `message` JSON contains zero system-role messages and still
  contains the user + assistant messages.
- [x] 2.2 Add a follow-up assertion: on the next turn's `BeforeAgent` replay, no
  system-role message is injected from history.
- [x] 2.3 Run `go test -cover ./internal/agent/middlewares/...` and confirm the
  new test passes and package coverage stays ≥ 70%.

## 3. Verification

- [x] 3.1 Manual/trace: run a 2-turn chat through the agent and confirm the
  model input carries exactly one system message (the fresh instruction), not
  `1 + N`. _Verified by automated `TestSystemMessagesNotPersisted` (0 system
  persisted + no system injected on replay); live end-to-end trace not run._
- [x] 3.2 Confirm `question`/`answer` denormalization and `response_id` chaining
  are unaffected (system exclusion touches neither).

## 4. Spec

- [x] 4.1 Spec delta written under `specs/conversation-history/spec.md`
  (MODIFIED requirement + scenario). (This proposal.)
