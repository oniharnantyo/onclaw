# Design: enhance-resumable-streams

## Context

The runtime already keeps every consumer-relevant fact in memory on the producer side: `runningTurn` tracks the in-flight model call's text partial and running tools (`manager_impl.go`), the task registry owns delegated-task lifecycle (`subagents/tasks.go`), and `Subscribe` delivers a catch-up snapshot to late subscribers (design D5). Two defects break the resume contract:

1. **Attachment race** — `client.chat.subscribe` attaches the JS listener after `await transport.chat.Subscribe()` resolves, while the backend queues the snapshot and starts its pump inside `Subscribe` (`chat.go`). The Wails event dispatch (`dispatchWailsEvent`) delivers to currently-registered listeners only, so the snapshot — first in a buffered channel, pumped before the RPC returns — is routinely dropped. The transcript cannot cover for it: the in-flight call's partial text is live-only by design.
2. **No re-fire path** — the snapshot fires only on fresh subscribe. A server-mode WebSocket blip (auto-reconnected by `@wailsio/runtime`, backend subscription survives) loses deltas permanently for an existing subscription; the client appends deltas, so the streamed text carries a hole until the durable message replaces it.

Constraints honored: the session log's design rule — *replaying the log never depends on delta granularity* — stays intact; deltas remain live-only and unpersisted. Both hosts (bridge, server WebSocket) share one frontend and one service surface, so everything here is transport-agnostic by construction.

## Goals / Non-Goals

**Goals**
- Re-entering (or reconnecting to) a mid-stream session reconstructs the exact in-flight state — text, reasoning, running tools, running tasks — deterministically, not by push-ordering luck.
- Missed deltas are detected exactly (counter comparison) and healed within one beacon interval.

**Non-Goals**
- Groups: durable-first (persisted rows replayed by refetch); nothing to resync.
- Delta persistence or cursors on the session log.
- Adopting `assistant-stream/resumable` (welded to the AI-SDK HTTP transport; buddies runs `useExternalStoreRuntime` over bridge/WS events).
- Startup sweep for turns killed by an application restart (separate producer-lifecycle change).

## Decisions

### D1 — Client attaches the listener before calling backend Subscribe (root-cause fix)

`client.chat.subscribe` reorders to: register `transport.onEvent(...)` first, then `await transport.chat.Subscribe(sessionID)`. Events emitted between backend registration and listener attachment cannot exist, so the attachment window closes completely. Early arrivals during a session load land in the existing `eventBuffer` and fold after the transcript page — the machinery already built for exactly this.

*Alternative considered:* backend-side replay buffer or delayed snapshot emission — pushes the fragility around instead of removing it, and still racy for the very first emitted event.

### D2 — One snapshot builder, one composite live-only event

A single builder serializes the turn's full in-flight state into one composite, live-only event — `KindStreamSync` carrying a `Stream` payload: the current call's text partial, the reasoning partial, running tools (id, name, arguments), and running task rows (id, description, status, progress). Both consumers use it:

- `Subscribe` catch-up (D5) emits it to the new subscriber — replacing today's synthetic text-delta/tool-activity pair, which cannot express reasoning or tasks and folds append-only.
- `Resync` emits it to **all** subscribers of the session, so every attached surface reconciles at once.

Synthetic ids are unique per emission (`snap-<turn>-sync-<n>`, process-wide counter): `seenEventIDs` dedupe would otherwise swallow every re-fire after the first — the old `snap-<turn>-partial` id never hit this only because it was minted once per subscribe.

*Alternative considered:* a replace flag on `KindTextDelta` — overloads the delta vocabulary and needs multiple events to express one atomic state; the composite keeps one id, one fold.

### D3 — Snapshot completeness: reasoning partial and running tasks

`runningTurn` gains a reasoning builder mirroring the text one: the drain's reasoning-delta branch (`runner.go`) feeds a `reasoningPartial` alongside `rt.appendPartial`; per model-call completion resets both. `TaskRegistry` grows a running-state accessor derived from its own register/finish bookkeeping (no new tracking), and the turn records the registry on `runningTurn` so the snapshot builder can read it.

*Alternative considered:* leaving reasoning out — reattaching mid-reasoning shows a blank stream until text starts, the exact bug class this change fixes.

### D4 — Liveness beacon with a delta counter

While a turn streams, a per-turn ticker goroutine broadcasts a live-only `KindStreamPing` every ~5 s carrying the turn id and a monotonic count of deltas emitted for the turn (maintained by the same tap that feeds the text partial, guarded by `toolMu`). The beacon stops at the turn's terminal (def via the turn lifecycle — no goroutine leak). Not persisted; unknown to old clients.

The counter turns loss detection from a heuristic into a comparison: silence is ambiguous (a long tool call emits no deltas by design), but a ping whose counter exceeds the client's received count proves a gap, and pings prove the pipe is alive through any model/tool silence.

*Alternatives considered:* silence-only watchdog — false-positives on long tool calls, late healing when tuned safe; a transport-reconnect hook — `@wailsio/runtime` `Events.On` exposes no socket state; a cursor on every delta — touches every emission for detection the beacon already provides within one interval.

### D5 — Resync verb

`Manager.Resync(ctx, sessionID)` re-runs the snapshot builder and broadcasts the `KindStreamSync` to the session's subscribers; `ChatService.Resync` exposes it as a bound call. Idle or unknown sessions are a safe no-op. Resync is cheap (mutex-guarded snapshot, one event) and side-effect-free: it never persists, never touches the running turn.

### D6 — Client trigger rules

The chat store requests resync for the active session when: (a) a beacon's counter exceeds the locally received delta count, (b) the session is busy and no beacon has arrived for 2× the beacon interval, or (c) the window regains focus while the session is busy. One resync in flight at a time (in-flight guard), so a burst of triggers cannot storm the backend. Focus reuses the existing `connectRunningSessions` wiring.

### D7 — Replace-fold in the transcript reducer

`entries.ts` gains a `KindStreamSync` case: mint-or-replace the streaming assistant entry from the payload's text (and the reasoning entry from its reasoning), reconcile running tool rows, and reconcile running task rows — replacing prior live state rather than appending. Because the composite is atomic, the fold cannot mix old and new partials. Terminal records finalize reconstructed rows exactly as the subscribe-time snapshot does today; the durable assistant message still lands exactly once. Beacons fold as no-ops (staleness tracking reads them before the fold).

## Risks / Trade-offs

- [Beacon ticker outlives the turn] → stopped via the turn's terminal path (`defer` on the turn lifecycle), mirroring how the drain goroutine is bound; a liveness test pins it.
- [Counter drift between tap and beacon] → the counter lives on `runningTurn` under the same mutex as the partial; the beacon only reads it, never writes.
- [Resync storm on a flaky pipe] → client-side in-flight guard + the trigger rules fire on transitions, not on a timer loop; server-side cost per resync is one mutex snapshot.
- [Multi-tab surfaces reconcile independently] → resync broadcasts on the session's event channel; each surface dedupes and folds its own store — no cross-surface coupling.
- [Old frontend against new backend] → unknown live-only kinds are ignored by today's fold (no-op), so the beacon/sync degrade silently; new frontend against old backend never sees them. Additive both ways.

## Migration Plan

No persisted-format change and no schema migration — everything new is live-only and additive. Rollback is reverting the code; transcripts are unaffected. Bindings regenerate for the new bound call (expected widening of `client.test.ts` mocks, as with prior binding additions).

## Open Questions

- Beacon interval (default ~5 s) and staleness multiple (default 2×) — tunable constants, settled during implementation against real turn latency; the spec pins the mechanism, not the constants.
