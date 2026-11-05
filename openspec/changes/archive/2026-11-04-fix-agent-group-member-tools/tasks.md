# Tasks

## 1. Composite group-turn tool surface (design D1)

- [x] 1.1 `internal/agents/runner.go` step 6: the group-turn branch resolves the ordinary surface via `m.resolveTurnTools(ctx, agent, rt.sessionID)` and appends `rt.group.tools` (no name collision — assert/guard the merge); delete `groupToolSurface` from `internal/agents/gate.go`. Verify: `go build ./...`.
- [x] 1.2 `internal/agents/manager_impl.go` (startGroupTurn comment) and `internal/agents/groupturn.go` (GroupTurnSource doc): rewrite "replace the ordinary catalog surface entirely" to the additive contract (design D4). Verify: `grep -rn "entirely" internal/agents/` is empty.

## 2. Workspace rides the group turn (design D2)

- [x] 2.1 `internal/agents/runner.go`: remove the group-branch `composer.sessionRel = ""` zero-clear so the member's session-relative workspace paths render for group turns; confirm the uploads mirror runs for group turns (it keys on `surface.agentDir`, now set) and the fs/reduction/plantask/subagent capability blocks apply unchanged. Verify: `go vet ./internal/agents/...`.

## 3. Tests (spec scenarios)

- [x] 3.1 `internal/agents/groupturn_test.go`: flip the "exactly the source's tools" assertion (line ~265) to catalog + group tools; wire the fixture's workspace tempdir so fs tools root somewhere real; new case — a group turn's `read_file` reads a file in the member's workspace; `group_history` present beside the catalog. Verify: `go test ./internal/agents/... -count=1`.
- [x] 3.2 `internal/groups` tests still green (group tools' behavior untouched). Verify: `go test ./internal/groups/... -count=1`.

## 4. Full gates

- [x] 4.1 `gofmt -w .`, `go vet ./...`, `go test ./... -count=1` all clean. Verify: fresh run output in the apply report.
