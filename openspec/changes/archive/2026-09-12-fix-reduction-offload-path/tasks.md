## 1. Reduction Middleware Root Directory Fix

- [x] 1.1 In `internal/agents/agent.go`, update `reduction.TypedConfig` construction to set `RootDir: backend.DefaultMountPoint` instead of `cfg.Filesystem.AgentDir`.

## 2. Regression Testing & Verification

- [x] 2.1 Add a unit test in `internal/agents/` verifying that when a tool produces output exceeding the reduction truncation threshold, the output is saved to `/workspace/trunc/<call_id>` on the jailed backend and is readable via `jail.Read`.
- [x] 2.2 Run the full test suite (`go test ./...` and `go test -tags=integration ./...`) to ensure all tests pass cleanly.
