# Proposal: detach-run-execution

## Why

A run's lifetime is coupled to its consumers. `Runner.execute` derives the run context from the caller's context — and the only HTTP path that triggers execution today (`ResolveApproval`) passes the request context, so the resumed turn is cancelled the moment the approval response is written. The same coupling exists for consumers: `EventStream` blocks its sender when the buffer fills and nobody drains it, so an unwatched or disconnected viewer stalls the run itself. For the product goal — start a task, leave, come back — execution must survive every hangup: request return, SSE disconnect, absent viewers.

## What Changes

- **RunManager owns execution contexts.** Run contexts derive from the server's base context (process lifetime, graceful-drain on shutdown), never from a request or stream context. The manager tracks live runs by identity (workspace, agent, session) and holds their cancel functions.
- **Disconnect ≠ cancel.** A caller that returns early (fire-and-forget start), an SSE client that closes the tab, and a poller that never subscribes all leave the run running. Only an explicit cancel stops it.
- **The event tap becomes droppable.** When no consumer drains the live `EventStream` (or the buffer fills), the runner drops tap events instead of blocking. Durable history (`session_events`) is unaffected — it is written by the session adapter inside the run, not through the tap. Catch-up consumers recover dropped tap events via `?after=`.
- **Fix the approval-resume bug.** `Resume` no longer inherits the HTTP request context; the resumed turn runs detached like any other run.
- **Run cancellation endpoint.** `POST /api/workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel` (or equivalent session-scoped form) cancels the active run for the session; cancellation semantics stay "at a safe point" as specced today.
- **Non-goal: restart survival.** In-flight runs still die with the process; HITL pauses survive via checkpoints as today. A durable run registry with boot-time re-dispatch is a deliberate follow-up, not part of this change.

## Capabilities

### New Capabilities

- (none — work lands in the existing `agent-runtime` capability)

### Modified Capabilities

- `agent-runtime`: execution lifetime detaches from callers and consumers; the live event tap may drop events when unwatched; cancellation gains an explicit run-cancel surface and explicitly excludes client disconnect as a cancel trigger.

## Impact

- **Runner signature stays stable.** `Run`/`Resume` still return `*EventStream`; the change is who owns the context and what happens when the stream is abandoned — callers (the future `/v1` facade, handlers, tests) are unaffected mechanically.
- **Graceful shutdown** drains in-flight runs (bounded wait) before exit; runs that do not drain in time are cancelled with a cancel marker, matching today's safe-point semantics.
- No schema, migration, or API-shape changes beyond the new cancel endpoint.
