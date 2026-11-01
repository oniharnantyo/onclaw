# Tasks

## 1. Reproduction

- [x] 1.1 Unit test in `internal/agents/runner_test.go`: streamed assembly where the identity frame carries CallID + name and argument frames carry empty CallIDs — assert one call with full arguments and no orphan (red first).

## 2. Implementation

- [x] 2.1 `toolCallAssembler.observeBlock`: empty-CallID blocks merge into the most recent in-progress call; `finish` defensively drops a residual empty-keyed entry.
- [x] 2.2 Confirm the persisted-record test path (fake model → drain → store assertions) covers arguments on the streamed fake, mirroring the existing duration assertions.

## 3. Verification

- [x] 3.1 `gofmt -w .` && `go vet ./...` && `go test ./internal/agents/...`.
- [ ] 3.2 Live smoke (Z.AI/GLM agent, env-gated or manual): run a shell turn, reload the session, confirm the reloaded card shows the command; spot-check new `session_events.tool_activity` rows carry `arguments`.
