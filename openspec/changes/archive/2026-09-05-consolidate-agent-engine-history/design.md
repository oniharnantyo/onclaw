# Design: consolidate-agent-engine-history

## Context

The September rewrite left the runtime in this shape:

```
internal/agents/
  agent.go        ← engine: Agent struct, load/resolve/composeAgent/execute/Run/streamRun
  agent_v2.go     ← composition: Config, validateConfig, buildMiddlewares, Compose,
                    skillBackend, offloadTranscript
```

Both were (re)written the same day; `_v2` is rewrite-generation naming, not an API version. The engine is wired in the composition root but explicitly unused (`_ = engine`, `internal/cli/server.go:150`). History is durably persisted as an append-only `session_events` log (migration 000016) whose payloads are serialized ADK `SessionEvent[*schema.AgenticMessage]` values (`schema.HumanReadableSerializer`); the ADK runner replays the full log per turn through `ADKSessionAdapter`. Nothing reads the log back for display.

## Goals / Non-Goals

- Goals: single-file runtime pipeline; a read API over persisted history shaped for the UI; engine finally consumed by the router.
- Non-Goals: no chat/SSE execution endpoint (separate change — this one deliberately stops at the read path); no web UI work; no schema/migration changes; no change to how the model receives context.

## Decisions

### D1: Promotion procedure — engine moves into agent_v2, old agent.go dies, rename

The user specified the direction: keep `agent_v2.go`'s body as the base, move the engine code into it, then delete the old `agent.go` and rename `agent_v2.go` → `agent.go` (and `agent_v2_test.go` → `agent_test.go`). Within the file the layout is: package doc-level composition types (`Config`, `Compose`, middleware builder, helpers) first, engine type and execution flow second — one readable top-to-bottom pipeline. No symbol renames beyond the filenames (`Compose`, `Config`, `Agent`, `NewAgent` keep their names; `Config` stays `Config`, not `AgentConfig`, to limit churn).

### D2: History model stays full replay — no previous_response_id

`previous_response_id` (OpenAI Responses-API chaining) appears nowhere in the repo and was considered as the per-turn history mechanism. Rejected: the session-event replay already works, is provider-agnostic (Claude/Gemini/OpenAI adapters all receive reconstructed messages), and survives compaction replay semantics already specified in `agent-runtime`. Chaining would couple the runtime to one provider's wire concept and add a per-turn `response_id` column for no capability we need. The user confirmed: keep replay, add the read API.

### D3: Translation layer lives in the engine package, shaped as TranscriptEvent

`History` reuses the existing `TranscriptEvent` vocabulary (`events.go`) rather than inventing a second wire shape — the SSE stream already emits these kinds, so the web app renders live and historical turns with one parser. Translation mapping:

| Stored ADK session event kind | Translated TranscriptEvent |
|---|---|
| message events (user input text) | `message_completed`, role `user` |
| message events (assistant gen text) | `message_completed`, role `assistant` |
| tool-call span start/end | `tool_call_started` / `tool_call_finished` (+name, call id) |
| messages-replaced (compaction) | `context_compacted` |
| cancel/interrupt markers | `cancelled` |

Each row's `occurred_at` and `turn_id` carry through; ordering is the log's `seq` order. `payload` stays opaque at the store layer — deserialization to typed ADK events happens inside `internal/agents` (which already owns `eventSerializer`).

### D4: Endpoint under the agents route group, workspace-scoped

`GET /workspaces/:ws/agents/:agent/sessions/:session/events?after=<event_id>&limit=<n>` — same group as existing agent routes, so workspace-scoping middleware and `agents.read` permission apply unchanged. `after` is the event-ID cursor (mirrors `LoadSessionEventsParams.AfterEventID`); `limit` specifies the maximum number of events to return (capped at 500; non-positive or non-numeric values return 400 Bad Request `limit must be a positive integer`); response is `{events: [...], next: "<event_id>"}` with `next` empty on the last page. Cross-tenant access yields 404 indistinguishable from unknown session (existing spec language). The engine is injected into `RouterOptions`; the handler calls `engine.History(...)`.

## Risks / Trade-offs

- **Merging two large files** makes `agent.go` long (~500 lines) — accepted; it is one coherent pipeline and the split had no seam value.
- **Deserializing ADK payloads for display** couples the read path to the serializer — accepted; the alternative (dual write of UI-shaped rows) duplicates state and was rejected. If ADK event shapes evolve, only the translation layer changes.
- **Endpoint before any chat endpoint** — the read API can ship first because history is already being written by the engine; the web app gets real data to build against.

## Migration Plan

Pure refactor + additive endpoint. No data migration. Order: merge files → translation layer + tests → handler/route wiring → verify.
