# Tasks

## 1. Version bump: eino v0.10.0-alpha.28 → v0.10.0-alpha.35

- [x] 1.1 Bump `github.com/cloudwego/eino` to v0.10.0-alpha.35 in go.mod, `go mod tidy`; `go build ./...` and `go vet ./...` pass. Verify: green build + vet on the bump commit alone.
- [x] 1.2 Run the full backend suite `go test ./...` and fix fallout from the riding behavior changes (expected friction: summarization skill-preamble ordering tests, session-reconstruction parallel tool-result ordering). Verify: `go test ./...` green.
- [x] 1.3 Run `./scripts/smoke.sh` against a fresh `onclaw_smoke` database (drop + recreate first) and the web suite. Verify: smoke fully green, web tests green.

## 2. Composition: subagent capability wiring

- [x] 2.1 Add Config fields — `SubAgents []adk.TypedAgent[*schema.AgenticMessage]`, `Background *SubagentBackgroundConfig` (Runner + OutputStore + OutputDir), `WithoutGeneralSubAgent bool` — and fail-fast validation: capability wired with suppression and zero declared subagents → error; duplicate/empty subagent names or empty descriptions → error. Verify: unit tests for each rejection and the no-capability no-op path.
- [x] 2.2 Implement the general-purpose clone builder: same instruction, model, resolved tools, iteration cap, and the pre-subagent handler slice; name `general-purpose`, research-delegate description; no delegation capability on the clone. Verify: unit test asserting the clone's tool surface excludes `agent`/`task_output`/`task_stop` and mirrors the parent's business tools.
- [x] 2.3 Extend `buildMiddlewares`: append `subagent.NewTyped` (ToolName `agent`, SubAgents = clone + declared, Background Local when configured) then `backgroundtaskmw.NewTyped` bound to the Runner's Manager, after filesystem and before attachments/hooks/gate/tool-error-result; assert the fixed order in the ordering test (modify the existing full-stack-ordering test). Verify: ordering test covers with-capability and without-capability compositions.
- [x] 2.4 Purity guard: a composition test asserting no store/database/disk access when the capability is wired (follow the existing purity test pattern). Verify: test green.

## 3. Background plumbing

- [x] 3.1 Implement the jail append-opener in `internal/agents/backend`: append-only opens under `<agentDir>/.tasks/`, traversal rejected, jail-relative path returned to the model. Verify: table test — happy path, traversal attempt rejected, missing dir created.
- [x] 3.2 Per-run composition in `composeAgent`: when the capability resolves, construct `backgroundtask.New` (nil config = in-memory stores) + `backgroundtask.NewExecutorRegistry` + `backgroundlocal.New`, wire OutputStore/OutputDir, pass Config fields through. Verify: unit test — two runs produce independent Managers/task-id spaces.
- [x] 3.3 Notification pump: per-run goroutine leasing the session's notifications from the Manager outbox and appending a completion transcript event (task id, outcome, output path) via the session-events machinery; render task-kind-appropriate copy (delegation vs shell); pump exits with the run. Verify: unit test — complete a background task, assert exactly one completion event lands with correct seq and payload.

## 4. Background shell lane

- [x] 4.1 Wire the fs middleware's background seam in `buildMiddlewares`: when the shell tool is wired AND the background-shell capability is selected, set `fsConfig.Background` with `Local{Runner: the same per-run Runner, OutputStore, OutputDir}` and `NotificationSessionID` resolving the parent session id; leave the foreground timer disabled (`ForegroundTimeoutMs` non-positive on the Runner config). Verify: unit test — execute tool schema gains `run_in_background` only with the lane on; foreground execution path unchanged (no timer) with the lane on.
- [x] 4.2 Runner resolution for the reserved `background_shell` name: capability attaches only when the effective allowlist names it AND the shell tool is wired; background-shell selected without shell → capability silently off at the runner (coherent resolution), while direct incoherent Compose calls fail fast (summarization-requires-filesystem precedent). Verify: unit tests — both present → lane on; background_shell alone → lane off, no error; direct Compose call with Background and no Shell → descriptive error.
- [x] 4.3 Shell lane end-to-end unit test: launch a background command via the managed execute path, assert the launch result carries the task id and output path; run `task_output` against the id and assert the command's output; run `task_stop` on a long sleep and assert canceled status; assert exactly one completion transcript event lands via the pump. Verify: test green.

## 5. Runner integration

- [x] 5.1 Reserved allowlist name `subagents`: resolve from the effective allowlist (respecting `AllowedTools` overrides and the workspace gate), strip it from the business tool surface, and gate the capability on it — mirroring the reserved `execute` handling. Verify: unit tests — selected → capability wired and name absent from the surface; unselected → byte-identical composition to today.
- [x] 5.2 Tool catalog rows for `subagents` and `background_shell` (display names, descriptions, groups) so the agent-config tool picker offers them; catalog test updated. Verify: catalog test lists both rows; API payload exposes them.
- [x] 5.3 Stream lane: drop events whose session variant is not the parent session id in the SSE translator (child-event filtering), keeping the `agent` tool call's own started/finished events. Verify: unit test — a delegation with inner child events yields exactly one started/finished pair on the stream and the hydrated transcript matches field-for-field.
- [x] 5.4 Token accounting: verify child model-call usage aggregates into the parent run's totals; fix attribution if it does not. Verify: unit test asserting run totals include delegation usage.
- [x] 5.5 Hooks interplay: test that a `pre_tool_use` hook matching `agent` blocks delegation with the canonical block result and the run continues. Verify: test green.
- [x] 5.6 Delegation background-control test: launch the `agent` tool with `run_in_background`, assert `task_output` on the returned id streams the child's transcript progress records newest-first while the child works, `task_stop` cancels the child run with a canceled-status launch lane, and foreground `agent` calls expose no task id at all. Verify: test green.

## 6. Docs, breakdown accounting, smoke

- [x] 6.1 Context breakdown: account the BeforeAgent-injected delegation instruction slice (add a lane if under-counted, following the skill-middleware precedent). Verify: breakdown test totals include the delegation instruction for opted-in agents and are unchanged otherwise.
- [x] 6.2 Promptdocs: add the "## Delegation & Background Work" section to `internal/promptdocs/AGENTS.md` with the verbatim copy pinned in design D12 (placed after "## Follow-through"), landing with the capability, not before; run the promptdocs sweep. Verify: sweep green; the BasePrompt-injection composer tests still pass (HasPrefix pin).
- [x] 6.3 Smoke scenario: stub-LLM script drives a delegation turn (agent tool call → child completion → final answer), a background delegation (launch + task_output poll), and a background shell command (launch + task_output). Verify: `./scripts/smoke.sh` fully green with the new sections.

## 7. Verification

- [x] 7.1 Full gates: `go build ./...`, `go vet ./...`, `go test ./...`, `./scripts/smoke.sh`, web suite. Verify: all green.
- [ ] 7.2 Live pass (user-gated): opt an agent into `subagents` and `background_shell`; delegate a research subtask, run a test suite with run_in_background and poll it, then a background delegation — confirm one card per delegation/command, task_output polling, completion events, restart honesty. Verify: manual pass notes recorded.
