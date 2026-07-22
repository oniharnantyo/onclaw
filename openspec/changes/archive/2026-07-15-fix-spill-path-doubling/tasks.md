# Tasks

## 1. Spill path construction

- [x] 1.1 In `internal/agent/tools/spill.go` `SpillArtifactPaths`, change the absolute dir to `filepath.Join(scope.Workspace, "sessions", sid, "tool_results")` and the relative path to `filepath.Join("sessions", sid, "tool_results", name)`. Verify: for `Workspace=~/.onclaw/workspace/master`, `SessionID=16`, the file lands at `~/.onclaw/workspace/master/sessions/16/tool_results/` with no repeated `.onclaw/workspace/master` segment.
- [x] 1.2 Remove the now-unused `agent := sanitizeComponent(scope.AgentName)` line in `SpillArtifactPaths` (the agent identity no longer participates in the spill path). Verify: `go vet ./internal/agent/tools/...` reports no unused variable.

## 2. Compaction detection

- [x] 2.1 In `internal/agent/summarization_scrub.go`, update `spillPathRe` to match the new `sessions/<session_id>/tool_results/…` shape (drop the `.onclaw/workspace/` prefix, keep the `tool_results/` anchor). Verify: the regex matches `sessions/16/tool_results/foo.md` and does not match ordinary prose.

## 3. Tests

- [x] 3.1 Update `internal/agent/tools/spill_test.go`: rewrite `TestSpillPathSanitization` (agent is no longer a path component — assert session/tool/title sanitization only), and change the `toolResultsDir` fixture in `TestSpillFallbackOnFileWriteFailure` to `<ws>/sessions/<sid>/tool_results`. Add a regression assertion that the path is not double-nested (no repeated `workspace/master`).
- [x] 3.2 Update the `spillPath` fixture in `internal/agent/summarization_scrub_test.go` (`TestScrubSpilledResultPathPreserved`) to `sessions/sess123/tool_results/…` and confirm the scrub still detects and preserves it verbatim.
- [x] 3.3 Confirm `internal/agent/tools/browser/browser_test.go` `TestBrowserScreenshotPersistsPNG` still passes with only the generic `tool_results` substring assertion; adjust only if it hard-codes the `.onclaw/workspace/<agent>` prefix.

## 4. Verification

- [x] 4.1 Run `make vet` (or `go vet ./...`) — clean.
- [x] 4.2 Run `go test ./internal/agent/tools/... ./internal/agent/...` — all green.
- [x] 4.3 Confirm statement coverage ≥ 70% for affected packages (`internal/agent`, `internal/agent/tools`).
