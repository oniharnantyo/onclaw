# Tasks: consolidate-agent-engine-history

## 1. Promote agent_v2.go to agent.go

- [x] 1.1 Move engine code from `internal/agents/agent.go` into `internal/agents/agent_v2.go`: `Agent` struct, `AgentOption`s, `NewAgent`, `resolvedContextWindow`, `load`, `resolve`, `composeAgent`, `execute`, `Run`, `streamRun`; arrange the file as composition (Config/Compose/middlewares/helpers) first, engine second
- [x] 1.2 Delete the old `internal/agents/agent.go`; rename `agent_v2.go` → `agent.go` and `agent_v2_test.go` → `agent_test.go` (merging any still-applicable engine tests)
- [x] 1.3 Sweep `internal/agents/` for symbols left unreferenced by the consolidation; remove dead files/code found (report anything beyond the two promoted files before deleting)
- [x] 1.4 `go build ./... && go vet ./... && go test ./...` green with no behavioral change

## 2. History translation layer

- [x] 2.1 Add `HistoryRequest{WorkspaceID, AgentID, SessionID, After, Limit}` and `HistoryResult{Events []TranscriptEvent, Next string}` types
- [x] 2.2 Add `History(ctx, HistoryRequest)` on `Agent` in a new `internal/agents/history.go`: load events via `store.SessionEventStore` (workspace-scoped, `AfterEventID`, `Limit`), deserialize with `eventSerializer`, translate per design D3 mapping into `TranscriptEvent`s in `seq` order with `occurred_at`/`turn_id` carried through
- [x] 2.3 Unit tests against the fake store: round-trip persist-via-`ADKSessionAdapter` → `History` returns user/assistant messages, tool-call spans, compaction marker; cursor pagination (`after`/`limit`, `next`); unknown session returns empty result

## 3. Read endpoint and wiring

- [x] 3.1 Add handler `GET /workspaces/:ws/agents/:agent/sessions/:session/events` parsing `after`/`limit` query params, calling `engine.History`, returning `{events, next}`; cross-tenant/unknown → 404 indistinguishable
- [x] 3.2 Inject the engine into `RouterOptions` and register the route; replace `_ = engine` in `internal/cli/server.go` with real wiring
- [x] 3.3 Handler tests: 200 with translated events, `limit`/`after` honored, permission/workspace scoping enforced (403/404 paths)

## 4. Verification

- [x] 4.1 Full `go build ./... && go vet ./... && go test ./...` (plus integration tests if DATABASE_URL available)
- [x] 4.2 Manual smoke: start server, seed `session_events` rows via one engine `Run` against a fake/local provider, curl the new endpoint and confirm transcript shape
