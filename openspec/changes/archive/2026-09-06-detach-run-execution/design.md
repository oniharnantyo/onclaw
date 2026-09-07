# Design: detach-run-execution

## Context

`Runner.execute` builds `runCtx, cancelRun := context.WithCancel(ctx)` from the caller's context and launches `streamRun` in a goroutine; the ADK runner, session adapter, and browser teardown all hang off `runCtx`. Callers today: tests and smoke (direct), `ResolveApproval` (HTTP request context — the resumed turn dies when the response is written), and the future `/v1` facade. `EventStream` is a 128-buffer channel whose `Send` blocks when full; only `Close` unblocks it, so an undrained stream wedges `drainAgentEvents` and, with it, the run. Persistence does not flow through the tap — the ADK session adapter writes `session_events`/`session_checkpoints` inside the run — so the tap is purely a live-view leg and is safe to drop from.

## Decisions

### D1 — RunManager keyed by session (chosen: in-memory manager over a registry)

A small `internal/agents` manager holds live runs keyed by (workspace, agent, session): context, cancel function, and terminal-state channel. `Run`/`Resume` register with it; explicit cancel and graceful shutdown address it.

```go
type RunKey struct{ WorkspaceID, AgentID, SessionID string }
type runManager struct {
    base    context.Context           // server base context; cancelled on shutdown drain
    mu      sync.Mutex
    live    map[RunKey]*liveRun       // cancelFn, done channel, turn id
}
func (m *runManager) start(span context.Context, key RunKey) (ctx context.Context, cancel func(), done <-chan struct{})
func (m *runManager) cancel(key RunKey) bool   // false when no live run
func (m *runManager) drain(timeout time.Duration)
```

- The run context derives from `base` — never from a request context. The one-session-one-run invariant (already implicit) makes the key sufficient; a second `Run` on a live session is rejected as conflict (409 at the HTTP layer).
- Registry (durable rows, boot-time re-dispatch) stays out — non-goal this change; `RunKey` and the manager interface are the seam where it would land later.
- One active run per session simplifies cancel addressing: no run IDs to mint yet. The `/v1` change's `resp_<session>_<turn>` encoding gives clients a stable handle without extra machinery.

### D2 — Tap drops when unwatched (chosen: drop-new over drop-oldest)

When the buffer is full and no consumer reads, `Send` discards the new event and returns immediately. Drop-oldest would keep the newest deltas flowing for a late subscriber, but a subscriber that arrives that late should be using `?after=` catch-up — drop-new keeps `Send` lock-free-cheap and preserves the invariant that the tap is best-effort, history is truth. `drainAgentEvents` counts drops for a debug log line; nothing else.

- Attached-consumer behavior: a consumer that keeps up with the run sees no drops and receives the full ordered event sequence, preserving the existing "deltas stream as they occur" requirement. A slow consumer that lets the buffer fill loses excess tap events — that is the spec-mandated drop-new behavior — and recovers them from history via `?after=` cursor catch-up; the tap is best-effort, history is truth.
- Persistence is untouched — it already bypasses the tap via the session adapter.

### D3 — Callers opt out of blocking on stream close (chosen: abandon-safe streams, no new API)

`streamRun` closes the stream in a defer, as today. The change is that abandoning the stream (never calling `Recv`, or calling `Cancel` on the stream) affects only the tap:

- `EventStream.Cancel` cancels the *view* subscription, not the run — it stops signaling the manager. (Today `cancelFn` is the run's cancel; it moves to the manager. The stream keeps a local close for its own channel.)
- The run-level cancel lives behind `runManager.cancel(key)`; the HTTP cancel endpoint and future `/v1` cancel route through it.

This keeps `Run`/`Resume` signatures and the caller contract stable: callers that want fire-and-forget simply ignore the stream.

### D4 — Approval resume detaches (chosen: manager-owned context at the handler seam)

`ResolveApproval` keeps its request context for the load/validate phase (`PendingApproval`, permission checks) but the `Resume` call registers the resumed turn with the manager, which derives context from `base`. Same seam for `Run`: the handler's request context covers validation only. `context.WithoutCancel` is not used — the manager is the single place contexts originate, so there is no partially-detached context to audit.

### D5 — Graceful shutdown drain (chosen: bounded wait, then cancel-with-marker)

On shutdown, the manager stops accepting new runs, waits up to a configured drain window (default 30s) for terminal states, then cancels stragglers. Cancelled runs record their cancel marker through the existing safe-point path, so history stays consistent for the next run on the thread. Un-drained HITL-paused runs need nothing — their pause state lives in checkpoints, not in the process.
