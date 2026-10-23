# Tasks

## 1. Backend: in-flight turn-state query

- [x] 1.1 Add `Manager.TurnState(ctx, sessionID) (busy bool, turnID string)` to the agents Manager interface and `manager` (reads `m.running[sessionID]`; parked approvals report busy with the parked turn's id; unknown session reports not busy) with table tests covering running, parked, and idle sessions. Verify: `go test ./internal/agents/ -run TurnState`.
- [x] 1.2 Expose `ChatService.TurnState(sessionID)` returning `{Busy, TurnID}` and regenerate Wails bindings once (`task common:generate:bindings --force`). Verify: binding file contains `TurnState`; `go build ./...`.

## 2. Backend: subscribe-time live-state catch-up

- [x] 2.1 Track in-flight tool calls on `runningTurn` (call id → name + args), added by the drain's record tap on the live running announcement and removed on the terminal record, mutex-guarded together with the existing `partial` accumulator; drain unit tests assert the map tracks a running call and clears on its terminal. Verify: `go test ./internal/agents/ -run Drain`.
- [x] 2.2 Emit the catch-up in `Manager.Subscribe` under `m.mu` when a turn is in flight: one live-only `KindTextDelta` carrying the accumulated partial (synthetic id `snap-<turnid>-partial`) and one running `KindToolActivity` per in-flight call (synthetic id `snap-<turnid>-tool-<callid>`); never persisted (direct channel send). Tests: subscribing mid-turn delivers partial + running tool before subsequent events; subscribing an idle session delivers nothing; snapshot events are absent from the persisted log. Verify: `go test ./internal/agents/ -run Subscribe`.
- [x] 2.3 Race test: a turn completing concurrently with Subscribe leaves the log with only durable events and the subscriber with either a full snapshot or none — never a torn one. Verify: `go test ./internal/agents/ -race`.

## 3. Frontend: subscription teardown split

- [x] 3.1 Split `client.chat.subscribe`'s cleanup into local-only handler removal and a new explicit `client.chat.unsubscribe(sessionID)` (transport seam + client). Verify: `cd frontend && npx vitest run src/lib/__test__`.
- [x] 3.2 `selectSession` stops unsubscribing the target before re-subscribing (backend replace owns the old sub); `selectAgent` pruning and `deleteSession` call the explicit unsubscribe. Store tests: switching away and back keeps the session's live feed delivering events (subscribe/unsubscribe call sequence asserted on a fake transport). Verify: `cd frontend && npx vitest run src/features/chat/__test__`.

## 4. Frontend: re-adopt the in-flight turn on re-entry

- [x] 4.1 `selectSession` buffers live events for the session being loaded and folds them after the transcript page (page first, then buffer, existing id dedupe; superseded switches drop the buffer). Store tests: an event arriving mid-load survives the page overwrite without duplicating a page event. Verify: `cd frontend && npx vitest run src/features/chat/__test__`.
- [x] 4.2 After the page lands, query `TurnState`; busy → adopt the returned turn id into `activeTurnID`, mark the session running, `setBusy()`. Store tests: re-entering a busy session sets busy with the real turn id and the terminal event settles it (stop returns to send, queue drains); re-entering after the terminal folded adopts nothing. Verify: `cd frontend && npx vitest run src/features/chat/__test__`.
- [x] 4.3 Mapping check: the catch-up snapshot folds through the existing reducer (partial text renders as a streaming entry; running tool row finalizes on the terminal record; no duplicate when the durable assistant message lands). Verify: `cd frontend && npx vitest run src/features/chat/runtime/__test__`.

## 5. Verification

- [x] 5.1 Full Go gates: `gofmt -w .`, `go vet ./...`, `go test ./... -race`. Verify: all green.
- [x] 5.2 Frontend gates and build: `vp fmt src`, `vp check src`, full vitest run, then `task build` (bindings + embedded dist fresh). Verify: all green, `task build` succeeds.
- [ ] 5.3 Live smoke (manual): send a message, switch to another session, switch back mid-stream — stop control visible, partial text and any running tool reconstructed, cancel lands a `turn_cancelled` terminal, and a reloaded transcript shows no snapshot rows. Verify: observed behavior recorded in the session notes.
