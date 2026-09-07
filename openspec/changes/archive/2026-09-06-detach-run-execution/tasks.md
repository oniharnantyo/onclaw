# Tasks: detach-run-execution

## 1. RunManager

- [x] 1.1 `internal/agents/runmanager.go`: `RunKey{WorkspaceID, AgentID, SessionID}`, `liveRun` (cancelFn, done chan, turnID), manager with `start`/`cancel`/`drain`; contexts derive from a base context supplied at construction; concurrent-start on one session returns conflict
- [x] 1.2 Runner integration: `Run`/`streamRun` and `Resume`/`streamResume` register the run with the manager; run context comes from the manager base, the caller's context governs only validation; second run on a live session errors with a conflict sentinel
- [x] 1.3 Wire base context + manager through `NewRunner` (option or positional param — follow the DI conventions in AGENTS.md) and bootstrap; graceful shutdown calls `drain` with the configured window before process exit

## 2. Droppable tap

- [x] 2.1 `EventStream.Send`: drop-new semantics when the buffer is full or the stream is closed (document the contract on the type); remove the blocking `select` on `done`
- [x] 2.2 `EventStream.Cancel` re-scoped to view-close only (close the channel, stop signaling the manager); run-level cancel moves to `runManager.cancel`
- [x] 2.3 `drainAgentEvents`: count dropped events, log at debug with turn/session context
- [x] 2.4 Tests: unwatched run with buffer overflow completes and persists (fake model, long event sequence); attached consumer still receives every event losslessly

## 3. Approval-resume detach

- [x] 3.1 `ResolveApproval`: request context scoped to load/validate only; `Resume` runs detached via the manager; regression test reproducing the bug (approval response returned before turn completes → turn still finishes and persists)
- [x] 3.2 Same seam applied where the future exec-start handler will call `Run` (handler-level test with an abandoned stream)

## 4. Cancel endpoint

- [x] 4.1 `POST /api/workspaces/:ws/agents/:agent/sessions/:session/runs/:turn/cancel` — permission-gated (agents.write), 404 unknown agent/session, 409 no live run for the session, maps the safe-point cancel path; returns `{cancelled: true}`
- [x] 4.2 Runner: `runManager.cancel` triggers the existing context-cancel path so the ADK run unwinds through the safe-point semantics (in-flight call completes or aborts marked, cancel marker recorded)
- [x] 4.3 Tests: cancel between tool calls (marker recorded, thread consistent); cancel of completed run → 409

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 5.2 Integration suite (`-tags=integration`) against `DATABASE_URL` green
- [ ] 5.3 `./scripts/smoke.sh` passes (approval section exercises the detached resume)
