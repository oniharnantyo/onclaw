# Proposal: consolidate-agent-engine-history

## Why

The agent runtime is split across two files whose names no longer describe their roles: `internal/agents/agent.go` (written first, then rewritten) holds the tenant-aware engine — loading, resolution, runner execution, event-iterator driving — while `agent_v2.go` holds the pure composition step (`Config` + `Compose` + middleware stack). The `_v2` suffix is scaffolding noise from the rewrite, and the split forces readers to reconstruct the full pipeline across two files. At the same time the engine is constructed but never consumed: `internal/cli/server.go` does `_ = engine // TODO: consume in chat/SSE handlers`, and although every conversation turn is durably recorded in the `session_events` log (full-history replay per turn via the ADK session adapter), there is no way to read that history back — no endpoint, no translation from the Eino-shaped event payloads to UI-shaped transcript data. The web app cannot render past chats until a read path exists.

## What Changes

- **Promote `agent_v2.go` to `agent.go`.** The engine code (Agent struct, options, `NewAgent`, `load`, `resolve`, `composeAgent`, `execute`, `Run`, `streamRun`) moves into the `agent_v2.go` file body, the old `agent.go` is deleted, and the file is renamed `agent.go` (tests likewise: `agent_v2_test.go` → `agent_test.go`). Result: one file containing the whole pipeline — load → resolve → compose (`Compose`, middleware stack) → execute (`adk.NewTypedRunner` + `ADKSessionAdapter`) → `streamRun` (typed event iterator → `TranscriptEvent`s). No behavioral changes; the runner + typed event-iterator integration that already exists is preserved in the promoted file.
- **Dead-file sweep.** After the merge, check `internal/agents/` for symbols/files left unreferenced by the consolidation and remove them. Current survey shows everything else is referenced; the only expected deletions are the old `agent.go` and the `_v2` filenames.
- **History read path (translation layer).** New `History(ctx, HistoryRequest)` on the engine: loads `session_events` through `store.SessionEventStore` (workspace-scoped, event-ID cursor pagination `after`/`limit`, kind filter), deserializes the stored ADK `SessionEvent[*schema.AgenticMessage]` payloads, and translates them into the existing UI shapes (`TranscriptEvent`, `CompletedMessage`, `ToolCallPayload`) — user/assistant messages, tool-call cards, compaction markers, cancel markers.
- **History read endpoint.** `GET /workspaces/:ws/agents/:agent/sessions/:session/events` with `after` and `limit` query params, served by a new handler, wired into the router. Injecting the engine into `RouterOptions` removes the `_ = engine` placeholder.
- **History model unchanged.** Model context continues to come from full per-session replay through the ADK session adapter. No `previous_response_id` is stored or sent — replay is provider-agnostic and already implemented; chaining would tie the runtime to one provider's API shape. (Decision recorded in design.md.)

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: a new "Transcript read API" requirement — workspace members can read a session's persisted transcript as UI-shaped events with cursor pagination, cross-tenant reads not found.

## Impact

- **Backend:** `internal/agents/agent.go` (consolidated file), `internal/agents/history.go` (new translation layer), `internal/server/handlers/agents.go` (new `ListSessionEvents`-style handler), `internal/server/router.go` (+ route, `RouterOptions` gains the engine), `internal/cli/server.go` (engine consumed instead of `_ = engine`).
- **No migrations:** `session_events` already stores everything the read path needs (kind, seq, payload, occurred_at); the endpoint reads, never writes.
- **Tests:** consolidated `agent_test.go` (composition tests merged), fake-store-based tests for the translation layer (round-trip: persist events via the session adapter → `History` returns UI-shaped events), handler tests (200 with events, cursor pagination, 404 cross-tenant).
- **Frontend:** none yet — the endpoint is the prerequisite for rendering chat history in the web app later; no web changes in this change.
