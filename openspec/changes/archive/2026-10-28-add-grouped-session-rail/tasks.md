# Tasks

## 1. Backend — turn in-flight observability (spec agent-runtime)

- [x] 1.1 `internal/agents/manager.go`: add `RunningSessions(ctx) []string` to the
  `Manager` interface (the current busy set — parked-on-approval turns included;
  empty, never nil, when idle) and a construction `Option` carrying the
  turn-transition callback (`func(sessionID string, busy bool)`).
- [x] 1.2 `internal/agents/manager_impl.go`: implement `RunningSessions` over the
  running map; emit busy=true after a successful `startTurn` claim and busy=false
  at every site that clears the record (`finishTurn` non-parked branch,
  `discardPending`). Parked turns produce no transition.
- [x] 1.3 Tests: transitions fire for UI, scheduler (`Submit`), and cancel paths;
  `RunningSessions` reconstructs the set while a turn (and a parked turn) is in
  flight.

## 2. Backend — all-sessions listing + bound surface (spec chat-view model)

- [x] 2.1 `internal/store/sessions.go` (+ sqlite impl + fake): `List(ctx)` over
  every session, most recent activity first, mirroring `ListByAgent`.
- [x] 2.2 `internal/sessions`: `Service.AllSessions` over the new store listing.
- [x] 2.3 `internal/services/chat.go`: bound `AllSessions()` and
  `RunningSessions()`.
- [x] 2.4 `internal/services/events.go`: `EventTurnsChanged` ("turns:changed") +
  `TurnsChangedEvent{sessionId, busy}`; `internal/cli/app.go` wires the runtime
  callback to the emitter; `main.go` registers the event.
- [x] 2.5 Update `agents.Manager` / store fakes in tests; `go vet ./...`,
  `go test ./...` green.
- [x] 2.6 Regenerate bindings (`task common:generate:bindings`).

## 3. Frontend store — all-sessions model (spec chat-view model)

- [x] 3.1 `lib/client.ts` + `lib/events.ts`: `client.chat.allSessions()`,
  `client.chat.runningSessions()`, `client.events.onTurnsChanged`.
- [x] 3.2 `features/chat/store.ts`: `sessions` holds every agent's rows;
  `selectAgent` fetches all sessions; `refreshSessions` refetches all; 
  `newSession(agentID?)` creates-and-opens through `selectAgent(agentID,
  sessionID)`; running set seeded from `RunningSessions()` at boot and fed by
  `turns:changed` (ingest path unchanged, idempotent).
- [x] 3.3 `deleteSession` keeps the follow-up selection inside the deleted
  session's agent (no cross-agent jump as a delete side effect).
- [x] 3.4 Store tests updated (all-sessions model, cross-agent activation,
  running-set push/query).

## 4. Frontend rail — grouped accordion grammar (spec chat-view)

- [x] 4.1 `SessionRail.tsx`: all-agent accordion — collapsed rows (avatar, name,
  session count, per-agent "+", group running dot), active agent's group
  expanded, context moves auto-expand; group order per design D5.
- [x] 4.2 Progressive "Load more sessions" per group (4 + active initial, +8
  batches, retires on exhaustion, hidden while searching); rail header "+"
  removed.
- [x] 4.3 Search spans every agent, expands matching groups, restores the
  collapse grammar when cleared.
- [x] 4.4 Keep: resize handle + width persistence, delete confirm/refusal toast,
  reduced-motion handling, nav-rail expand row, hidden below `md`.
- [x] 4.5 Rail tests rewritten for the grouped grammar.

## 5. Verification

- [x] 5.1 `gofmt -w .`, `go vet ./...`, `go test ./...` green.
- [x] 5.2 Frontend lint gate (`vp check src`) and chat feature vitest green.
- [x] 5.3 `openspec validate add-grouped-session-rail --strict` green.
