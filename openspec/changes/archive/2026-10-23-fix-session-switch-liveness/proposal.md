# Proposal

## Why

Switching away from a session with a turn in flight and back loses the turn: the transcript rebuilds from durable events only (streaming deltas and running-tool announcements are live-only), so the in-flight work is invisible until the next durable milestone, and the store resets `busy` to idle while the backend turn keeps running — hiding the stop control and making sends fail with "turn in progress". A Subscribe/Unsubscribe race on re-subscription can additionally leave the opened session with a dead live feed entirely. Stop therefore "does not work" exactly in the switch scenario, and the landed-on session appears frozen.

## What Changes

- Re-adopt an in-flight turn on session re-entry: the chat store asks the backend whether the opened session has a turn in flight (and its turn id) and re-arms the local turn lifecycle, so the stop control shows, the queue behaves, and the terminal event settles state normally.
- Add the missing runtime surface: a bound turn-state query (busy + in-flight turn id) on the agents manager, exposed through the chat service and generated bindings.
- Fix the session re-subscription race: the frontend no longer fires `Unsubscribe` against the session it is about to re-subscribe to (the backend's Subscribe already replaces the previous subscription); explicit unsubscribe is reserved for real teardown paths.
- Fix the load-overwrite window: live events (including the new catch-up snapshot) that arrive while the initial transcript page is being fetched are buffered and folded after the page, instead of being discarded by the page's state overwrite.
- Add a subscribe-time live-state catch-up: when a session with a turn in flight is subscribed, the manager emits the in-flight turn's partial text and running tool calls as live-only events, so a re-entered transcript reconstructs the in-flight state immediately instead of waiting for the next durable milestone.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `chat-view`: new requirement pinning session re-entry liveness — a re-entered session with a turn in flight shows the stop control and working state, reconstructs in-flight partial text and running tool activity, keeps its live feed after the switch, and settles on the turn's terminal.
- `agent-runtime`: new requirements for the in-flight turn-state query (busy + turn id, approval-parked included) and the subscribe-time live-state catch-up (partial text + running tool calls delivered to late subscribers as live-only events, never persisted).

## Impact

- Backend: `internal/agents` (manager turn-state query, in-flight live-state snapshot at subscribe, per-turn record of running tool calls), `internal/services/chat.go` (new binding). Regenerated Wails bindings required (`task common:generate:bindings --force`).
- Frontend: `frontend/src/lib/transport.ts` + `client.ts` (turn-state call, unsubscribe split out of the subscribe cleanup), `frontend/src/features/chat/store.ts` (re-adopt on selectSession, teardown-only unsubscribe, load buffering).
- No migrations; no protocol changes beyond one new binding and live-only snapshot events that existing dedupe/folding already accepts.
