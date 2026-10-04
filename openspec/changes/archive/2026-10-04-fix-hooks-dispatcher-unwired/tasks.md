# Tasks

## 1. Failing-first regression pin

- [x] 1.1 Add the integration test that boots through the production composition path (composition-root-built `RuntimeOptions.Runner`, not a test-local runner), stores an enabled workspace `pre_tool_use` command hook matching a built-in tool, drives a `/v1` turn that invokes the tool, and asserts the canonical `blocked_by_hook` tool result, the absent side effect, and a hooks-execution audit row. Verify: the test FAILS on the pre-fix tree with the empty-chain signature (tool succeeds, zero audit rows).
- [x] 1.2 Add a boot-level guard test: the runtime assembled by the composition root serves a runner whose `HasHooks()` is true when a hook exists (or equivalent seam exposure added minimally for testability — prefer the behavioral 1.1 assertion; only add a seam if 1.1 cannot isolate the wiring failure). Verify: test fails on the pre-fix tree, passes post-fix.

## 2. Wiring fix

- [x] 2.1 In `internal/cli/server.go`: build the shared `agenthooks.NewRegistry(...)` (encryption key, `WithCommandEnabled`/`WithScriptEnabled` from config, MCP invoker, evaluator factory — same shape as router.go:462) and add `agents.WithHooks(agenthooks.NewDispatcher(st.Hooks(), hookRegistry))` to the `NewRunner` call. Ship the registry (or its constructor inputs) so the hook handlers and the runner share one instance. Verify: `go build ./...` green.
- [x] 2.2 In `internal/server/router.go`: delete the `if runner == nil` fallback runner construction (~lines 521–620) and take `RuntimeOptions.Runner` as required (per house non-nil-injection rule); keep the hook handlers wired to the shared registry; move any option from the deleted list that the CLI builder lacks (audit both lists field-by-field before deleting — the drift audit is the point). Verify: `go build ./...` green; no second `agents.NewRunner` site remains in the shipping path.
- [x] 2.3 Rehome tests/fixture boots that relied on `Runner: nil` reaching the fallback: point them at a test composition helper or construct the runner explicitly. Verify: `go test ./...` green.

## 3. Verification

- [x] 3.1 Turn 1.1/1.2 green; `go vet ./...`, `go test ./...`, `go test -tags=integration ./...` all green. Verify: CI-equivalent local run clean.
- [x] 3.2 Smoke: add a smoke section — against a live server, create a workspace `pre_tool_use` command hook, drive a matched `/v1` turn, assert the blocked tool result; delete the hook. Verify: `./scripts/smoke.sh` fully green including the new section.
- [ ] 3.3 Web: no changes expected; confirm the hooks settings pane and executions audit render the live deliveries (manual pass in dev UI: hook create → chat turn in the same workspace → execution row visible, tool card shows the block payload). Verify: manual pass recorded in the change notes. (Server-side proven live 2026-10-04: probe turn produced decision=block audit row + honest agent answer; only the pane rendering eyeball remains, user-gated.)

## 4. Release note + bench cycle

- [x] 4.1 Changelog entry: hooks begin enforcing on deploy (breaking-by-fix); workspaces with stored hooks (and instance builtin/managed hooks) will see them fire; command-handler kill switch now gates production. Verify: entry present in the release notes file.
- [ ] 4.2 Bench hygiene + post-fix cycle: purge the bench fixture agent's memory (contaminant: "session filing rule" observed rerouting writes), then run the full cycle per `bench/README.md` — one run, `compare` against `bench/baselines/8565453-1.json` with the pre-registered noise-derived gates. Verify: compare report recorded next to the baseline; expected signature: tool-error lane moves (its 9 hard-fail hook trials become real measurements); record pass/fail honestly — a move inside the noise band is inconclusive, not a win. (Memory purge + cycle LAUNCHED 2026-10-04 at archive time, ~10h background run; results land in bench/baselines/ and the session memory.)
