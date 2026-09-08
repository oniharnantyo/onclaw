# Design: live-run-reattach-and-catchup

## Context

See `proposal.md` for motivation. Today, execution is detached in `runManager`, meaning backend runs survive HTTP request cancellations. However:
1. `streamRun` produces to a single `*EventStream` created per `Run()` invocation. If the initial consumer's HTTP connection terminates, `EventStream.Send` drops deltas because the channel is unread or closed.
2. `GET /workspaces/:ws/agents/:agent/sessions/:session/events` returns a static snapshot of already-committed events in PostgreSQL (`session_events`).
3. `threadPersistence.ts` debounces storage writes by 350ms, causing immediate page reloads after message send to lose newly minted session bindings.
4. On page load, `ChatRoute.tsx` calls `hydrateSession` once. If an agent is in the middle of executing a 30s tool or multi-step reasoning turn, the client sets `running: false` and stops listening.

## Goals / Non-Goals

**Goals:**
- Enable reconnected browsers, multiple browser tabs, or reloaded pages to seamlessly catch up on missed transcript events and stream live deltas to completion.
- Eliminate the 350ms debounce race on session-binding persistence so immediate reloads always retain the session identity.
- Provide a clean SSE streaming interface on the native session events endpoint (`GET /workspaces/:ws/agents/:agent/sessions/:session/events?stream=true&after=<seq>`).
- Seamlessly transition from PostgreSQL historical replay to in-memory live broadcast when an active run is in flight.

**Non-Goals:**
- Cross-workspace broadcasting or global pub/sub infrastructure (everything is in-process and workspace-scoped).
- Re-executing already-completed turns.

## Decisions

### D1 — Multi-Subscriber Broadcaster in `liveRun`
*(Chosen: In-memory fan-out with dynamic registration over single stream)*

`internal/agents/runmanager.go`'s `liveRun` struct will manage a collection of subscriber `*EventStream`s:
```go
type liveRun struct {
    cancel      func()
    agentCancel adk.AgentCancelFunc
    done        chan struct{}
    
    mu          sync.Mutex
    nextSubID   uint64
    subscribers map[uint64]*EventStream
}
```
- When `streamRun` drains events, it calls `liveRun.Broadcast(ev *TranscriptEvent)`.
- Reconnected callers invoke `runManager.Subscribe(key RunKey) (subID uint64, stream *EventStream, ok bool)` to attach a fresh stream.
- When an SSE request finishes or client disconnects, `runManager.Unsubscribe(key, subID)` cleans up the tap without affecting the execution or other consumers.

*Alternative considered:* Re-spawning runs or keeping a single shared channel. Single shared channel cannot fan-out to multiple reconnected requests or tabs.

### D2 — Two-Phase Replay & Live Handshake in Session Events Endpoint
*(Chosen: Sequential DB drain then in-memory live stream with deduplication)*

When a request arrives at `GET /workspaces/:ws/agents/:agent/sessions/:session/events?stream=true&after=<seq>`:
1. **Phase 1 (History Catch-up):** The handler queries `session_events` from the DB for events with `seq > after` (or chronological replay) and writes them to the SSE stream.
2. **Phase 2 (Live Tap Attachment):**
   - The handler attempts to subscribe to `runManager` for the `(workspaceID, agentID, sessionID)`.
   - If active, it subscribes before or during DB catch-up to avoid missing intermediate events, deduplicating any overlapping events by `EventID`/`TurnID`.
   - The handler pumps live events over SSE until the terminal event (`turn_completed`, `error`, `cancelled`).
3. **Phase 3 (Stream Termination):**
   - When the run completes (or if no run was active after DB replay), the endpoint emits `data: [DONE]\n\n` and closes the connection.

*Alternative considered:* Polling DB every 1s. Polling adds database load and loses chunk-by-chunk real-time text/reasoning streaming.

### D3 — Immediate Storage Flush on Message Dispatch
*(Chosen: Synchronous `savePersistedThreads` in `pushMsg` / `ensureSessionBinding`)*

In `web/src/store/index.ts` and `web/src/store/threadPersistence.ts`:
- When `ensureSessionBinding` or `pushMsg` is invoked, `persistAllThreads` is called synchronously in addition to the debounced background timer.
- This ensures that `localStorage.getItem('onclaw.threads.v1')` has the latest `sess_<uuid>` binding before network dispatch begins.

### D4 — Client Chat Hydration with In-Flight Auto-Stream
*(Chosen: Hybrid initial snapshot + streaming catch-up)*

In `web/src/screens/ChatRoute.tsx` and `web/src/lib/livechat.ts`:
- On chat mount, `hydrateSession` fetches the current transcript.
- If the last message or event indicates an incomplete/in-progress turn (or `runManager` reports an active run), the chat runtime opens an EventSource/SSE connection to the streaming session events endpoint.
- Live deltas, tool cards, and reasoning blocks update the active agent message in the Zustand store until completion.

## Risks / Trade-offs

- **[Risk] Race between DB event write and live stream tap:** An event might be written to PostgreSQL right as the handler switches from DB replay to live stream.
  - *Mitigation:* The handler registers the live subscriber *before* running the final DB catch-up query, and uses a seen set of `EventID`s to deduplicate events that arrive on both paths.
- **[Risk] Stale subscriber leak on abandoned connections:** If a client abruptly closes without triggering a normal cleanup defer.
  - *Mitigation:* The handler context cancellation `c.Request.Context().Done()` immediately invokes `runManager.Unsubscribe`.
