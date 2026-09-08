# Proposal: live-run-reattach-and-catchup

## Why

When a user refreshes the browser, closes a tab, or temporarily drops network connection during an active chat turn, the detached backend execution continues running and persisting events to PostgreSQL, but the client loses connection to the live tap. On reload, the UI sits idle because `fetchSessionTranscript` performs only a static point-in-time fetch without re-attaching to the in-flight run or catching up missed deltas. Furthermore, debounced local storage persistence (350ms) risks dropping client-minted session bindings on immediate reloads. Users experience this as an aborted or cancelled turn, violating the core contract that agent executions outlive caller disconnects.

## What Changes

- **Synchronous Session Persistence:** The web client writes minted `sess_<uuid>` session bindings to local storage immediately and synchronously on message send, eliminating the debounce race on quick page refreshes.
- **Run Manager Event Broadcaster:** `internal/agents/runManager` expands `liveRun` to support multiple concurrent subscribers (`EventStream` taps), allowing reconnected clients to attach to an executing turn.
- **Session Events Streaming / Re-attachment Endpoint:** `GET /workspaces/:ws/agents/:agent/sessions/:session/events` (and/or an OpenResponses stream endpoint) supports SSE streaming (`stream=true`), accepting an `after` event/sequence cursor. It replays persisted events since `after` from the database, seamlessly switches over to the live broadcast tap if the run is still active in memory, and completes cleanly when the run finishes.
- **Client Auto-Reattach & Hydration Lifecycle:** `ChatRoute` and `chat/runtime` detect if the active session has an unfinished turn on mount or during hydration, re-establishing an SSE stream to catch up and render in-flight reasoning, text deltas, and tool cards in real time until completion.

## Capabilities

### Modified Capabilities
- `agent-runtime`: Enhances the live event tap to support multiple concurrent subscriber taps per active run, and formalizes catch-up replay alongside live switchover for reconnected clients.
- `web-app/chat-runtime`: Extends the chat runtime and hydration lifecycle to immediately persist session bindings on send and auto-reconnect to in-flight runs via streaming catch-up across page reloads.

## Impact

- **Backend:** `internal/agents/runmanager.go`, `internal/agents/runner.go`, `internal/agents/stream.go`, `internal/server/handlers/agents.go`, `internal/server/handlers/v1.go`.
- **Frontend:** `web/src/store/index.ts`, `web/src/store/threadPersistence.ts`, `web/src/lib/livechat.ts`, `web/src/screens/ChatRoute.tsx`, `web/src/chat/runtime.tsx`.
- **API:** `GET /workspaces/:ws/agents/:agent/sessions/:session/events` accepts `stream=true` and `after=<event_id_or_seq>` parameters with `text/event-stream` response.
