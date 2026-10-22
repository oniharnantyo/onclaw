# Tasks

## 1. W1 — reduction + plantask

- [x] 1.1 Reduction adapter: jail-backed reduction Backend over the agent workspace, config wired to `m.contextLimit` (truncation length bound + clear budget, retained-round suffix), composed after patchtoolcalls and before skill. Verify: fakemodel unit test — an oversized tool result becomes a notice on the transcript with the content stored in the workspace and readable back; offload failure keeps the original.
- [x] 1.2 Plantask adapter: plantask Backend over the jail, BaseDir inside the agent workspace, middleware composed in the fixed order. Verify: unit test — task_create → task_update → task_list roundtrip persists across simulated turns in a temp workspace.
- [x] 1.3 Gating: catalog rows for `task_create`/`task_get`/`task_update`/`task_list` replace the todo rows; the four names gate by name through the turn's resolved surface (generalizing the fsDisabled mechanism); delete `tools/todo.go` and its tests. Verify: gate tests — globally disabled or allowlist-omitted agent is offered none of the four; `go test ./...` green; repo grep finds no `todo_write` references in tool offering/gating paths outside changelog/spec docs and historical transcript renderers.
- [x] 1.4 Frontend surfaces: Tools pane card list and agent create/edit allowlist chips list the four task tools in place of the todo pair. Verify: component tests updated, `vp check src` green.

## 2. W2 — subagent + backgroundtask

- [x] 2.1 Spike: compose `subagent.New` + `backgroundtask.New` against the fakemodel with one foreground and one background delegation; confirm Manager task lifecycle, Local OutputStore/OutputDir/TranscriptFormat behavior, and the injected control tools. Verify: spike test green and a written parity checklist mapping today's taskRegistry behaviors to Manager equivalents.
- [x] 2.2 Swap: rebuild the delegation on the middlewares — parent-clone SubAgent from the parent's finished data (no delegation tools in the clone surface), per-run Manager as the task-ID space, `TranscriptFormat` reproducing the `.tasks/` entry shape, taskRegistry retained only as the bridge emitting `sessions.TaskActivity` onto the parent transcript. Verify: existing `subagent_test.go` scenarios pass unmodified in intent — foreground result returns to parent, background collect via task_output, cancel via task_stop, transcript materialized under `.tasks/`, task_activity records on the parent transcript, disabled agent gets no agent tool, clone cannot recurse.
- [x] 2.3 Delete replaced hand-rolled code: `subagent_tools.go` construction and the Manager-superseded parts of `subagent_tasks.go`/`subagent.go`. Verify: `go test ./...` green; repo grep finds no orphaned references.
- [x] 2.4 Child composition parity: children share the parent's shell-approval checkpoint store and resolved hook chain. Verify: shell-approval and hooks subagent tests pass unchanged.

## 3. W3 — summarization safety net

- [x] 3.1 In-run summarization config: TokenCounter on the shared estimator, trigger at ~0.9 of the model context limit, default finalize, composed between reduction and skill. Verify: unit test — a turn whose state crosses the trigger mid-run is summarized and completes within the limit with no checkpoint write, no marker event, and no transcript truncation.
- [x] 3.2 Layering tests: pre-turn checkpoint compaction fires independently of the in-run path at the 0.8 threshold. Verify: `compact_test.go` extended with a both-layers scenario pinning checkpoint boundary arithmetic.

## 4. W4 — package split + bindings

- [x] 4.1 Extract `compose/` (compose.go, gate.go, resolver.go, context.go and their tests; handler-order wiring). Verify: `go build ./...` and `go test ./internal/agents/...` green.
- [x] 4.2 Extract `run/` (runner.go, turn.go, turninput.go, resume.go, subscription.go and their tests). Verify: same.
- [x] 4.3 Extract `compaction/` (compact.go + summarization config) and `subagents/` (the shrunken delegation adapter). Verify: same.
- [x] 4.4 Seam sweep: export the cross-package seams (`toolErrorResult`, `resolvedTools`, drain values), rewrite root `doc.go` as the package map. Verify: `go vet ./...` clean, no import cycles.
- [x] 4.5 Bindings + frontend: `task build`, then `task common:generate:bindings --force`, update frontend imports for moved bound types. Verify: `npm run build`, frontend typecheck, and `vp check src` all green.

## 5. W5 — verification

- [x] 5.1 Repo gates: `gofmt -w .`, `go vet ./...`, `go test ./...`. Verify: all green.
- [x] 5.2 Live smoke in server mode with a real model: a turn with an oversized tool result leaves a notice plus a workspace file; a task plan roundtrips across turns; a background delegation collects via task_output; a forced long turn triggers the in-run safety net without a checkpoint write. Verify: transcript and workspace inspected in the smoke session.
- [x] 5.3 Spec-statement drift check: diff each delta's statements (not just headers) against the implemented behavior — strict validate misses statement regressions inside MODIFIED blocks. Verify: manual diff recorded in the change notes.
