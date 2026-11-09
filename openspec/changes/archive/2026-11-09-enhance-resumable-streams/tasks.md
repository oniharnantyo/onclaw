# Tasks

## 1. Subscribe ordering fix (incident root cause)

- [x] 1.1 Reorder `client.chat.subscribe` (frontend/src/lib/client.ts) to attach the `transport.onEvent` listener before `await transport.chat.Subscribe(sessionID)`, so backend events for the new subscription — the catch-up snapshot included — can never be dispatched before a listener exists; verify with a unit test asserting listener registration precedes the Subscribe binding call
- [x] 1.2 Add the incident regression test at the store level: selectSession while a turn is mid-stream, subscribe mock emits the catch-up snapshot synchronously when Subscribe resolves, and the transcript's streaming entry contains the full partial text; verify it fails against the old ordering and passes after 1.1

## 2. Snapshot completeness in the runtime

- [x] 2.1 Track the in-flight call's reasoning partial on `runningTurn` (append/reset under `toolMu`, mirroring the text partial), feed it from the drain's reasoning-delta branch in internal/agents/runner.go, and reset it on the reasoning's durable record; verify with a runner/manager unit test that a turn streaming only reasoning reports a growing reasoning partial
- [x] 2.2 Add a running-task accessor to `TaskRegistry` (internal/agents/subagents/tasks.go) deriving the currently running tasks (id, description, status running, progress) from its existing register/finish bookkeeping; verify with a subagents unit test covering register → snapshot shows running → finish → snapshot empty
- [x] 2.3 Make the running turn's snapshot reach the task registry (record the registry on the turn or resolve it at snapshot time) so the snapshot builder can read running tasks; verify with a manager test that a turn with a running delegated task exposes it in the in-flight state

## 3. Composite in-flight snapshot event

- [x] 3.1 Add the live-only `KindStreamSync` kind and its `Stream` payload (text partial, reasoning partial, running tools, running tasks) to internal/sessions, including the live-only kind handling in the events switch; verify with a sessions unit test that the kind is recognized and never persisted (append validation)
- [x] 3.2 Build the snapshot from one builder and emit it from `Subscribe` in place of the synthetic text-delta/tool-activity pair, and extend the catch-up tests (TestManager_Subscribe_MidTurnCatchUpSnapshot and additions) for reasoning and running-task reconstruction; verify the manager test suite passes with the composite event
- [x] 3.3 Assert catch-up deliveries carry unique synthetic identifiers across emissions so consumer dedupe cannot swallow a later delivery; verify with a manager test that two snapshots for one turn arrive with distinct ids

## 4. Resync verb

- [x] 4.1 Implement `Manager.Resync` — a busy session builds the composite snapshot and broadcasts it to ALL of the session's subscribers; idle or unknown sessions are a safe no-op; verify with a manager test that two concurrent subscribers both receive a resync delivery and an idle session delivers nothing
- [x] 4.2 Expose `Resync` as a bound `ChatService` call (internal/services/chat.go); verify with a service test that the call forwards to the manager
- [x] 4.3 Regenerate the Wails bindings for the new bound call and event models (widening client.test.ts mocks as binding additions do); verify `go generate`/bindings build and the frontend typecheck passes

## 5. Liveness beacon

- [x] 5.1 Add the live-only `KindStreamPing` kind (turn identifier plus monotonic per-turn delta count maintained by the drain's tap under the same mutex as the partial) and a per-turn ticker that broadcasts it roughly every 5 seconds while the turn streams and stops at the terminal; verify with a manager test using a short interval that pings flow, the count is monotonic, and no ping follows the turn's terminal
- [x] 5.2 Settle the tunables as named constants — beacon interval 5s, client staleness window 2× — recorded in one place for both ends to import; verify by review that both frontend and backend read the same constants

## 6. Frontend fold and resync triggers

- [x] 6.1 Add the `resync` method to the client chat surface (frontend/src/lib/client.ts) and the new live-only event kinds to the session event vocabulary; verify the frontend typecheck passes
- [x] 6.2 Add the `KindStreamSync` case to the transcript fold in frontend/src/features/chat/entries.ts — mint-or-replace the streaming assistant entry from the text partial and the reasoning entry from the reasoning partial, reconcile running tool rows and running task rows, replace rather than append — and fold beacons as no-ops; verify with entries unit tests: replace after existing deltas, mint when absent, terminal records finalize reconstructed rows, beacons change nothing
- [x] 6.3 Wire the resync triggers in frontend/src/features/chat/store.ts — beacon counter mismatch, staleness beyond the window while busy, window focus while busy — behind a one-resync-in-flight guard; verify with store tests using fake timers that each trigger fires exactly one resync and a burst collapses to one

## 7. Integration

- [x] 7.1 Run the full Go gate (`gofmt -w .`, `go vet ./...`, `go test ./...`) and the frontend suite (`vp check`, tests) — rerun-once known flakes per project protocol; verify both suites green
- [x] 7.2 Live-smoke the incident: `task run`, start a long turn, switch to another session and back mid-stream — the text resumes exactly where it left with no gap and no doubling; then while a second surface (server mode) rides the same busy session, drop and restore connectivity and confirm the beacon-driven resync heals the stream; record the observation
