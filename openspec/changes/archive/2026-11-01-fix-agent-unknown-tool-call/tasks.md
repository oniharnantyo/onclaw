# Tasks

## 1. Backend — unknown tool call feeds back

- [x] 1.1 `internal/agents/compose/compose.go`: wire the per-turn `ToolsNodeConfig` with `UnknownToolsHandler` returning a non-nil error (`unknown tool %q; use one of the tools offered in this request`) — the handler must only ever return an error (its raw return would bypass the error-result shape); `ToolErrorResult` in the handler list converts it like any tool failure (design D1). Verify: `go build ./...`.
- [x] 1.2 `internal/agents/compose/toolcallguard_test.go` (suite style of the package): a fake model response mixing one known and one unknown call → the known tool executes, the unknown call is answered with the error payload naming it, the next model call receives the result sequence with both call ids answered once, and the turn loops to a final answer; an all-unknown response (the live incident shape) loops the same way; an all-known response behaves exactly as before. Verify: `go test ./internal/agents/compose/...`.
- [x] 1.3 `internal/agents/` turn-level test (fixture style of attachments_turn_test.go): a model that first calls `list` (unknown) then corrects to `ls` completes the turn with a final answer, the transcript records the unknown call exactly once as failed, and no duplicate record exists for the call id. Verify: `go test ./internal/agents/...`.

## 2. Verification

- [x] 2.1 Full gates: `gofmt -w .` on touched files only, `go vet ./...`, `go test ./...` in the repo root. Verify: all green.
- [ ] 2.2 Live smoke (GUI, `task dev`): on the Scout/PoolSide session, a prompt that previously triggered a hallucinated tool name completes the turn — the model retries with a real tool name after the synthetic error, and the transcript shows the failed unknown-call record. Verify: screenshot/notes in `.team/smoke/fix-agent-unknown-tool-call/`.
