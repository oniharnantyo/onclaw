# Proposal

## Why

Lifecycle hooks are a **no-op in every production deployment**. The bench's tool-error lane exposed it: all three hook-block tasks (`tool-hook-write-recovery`, `tool-hook-shell-to-ls`, `tool-block-plus-deny`) scored 0/3 in **both** the baseline-0 and the unchanged-harness dry-cycle run — not model noise, a hard ceiling. Live reproduction on the dev server (2026-10-04) confirmed the mechanism: a workspace `pre_tool_use` command hook (matcher `write_file`, then matcher `*`) never gates a `/v1` turn — the write/list executes successfully, the hooks-executions audit records **zero** rows, and a `run_started` hook at the runner seam never fires either. The resolved hook chain is **empty at `Runner.run`** even with enabled, healthy hooks in the database on the same workspace the run executes in.

Root cause (code-verified): there are **two `agents.NewRunner` construction sites with diverging option lists**, and the one that serves production is the wrong one.

- `internal/cli/server.go:551` builds the runner the composition root actually ships (`RuntimeOptions.Runner`, cli/server.go:727). Its option list contains **no `agents.WithHooks`** — the runner keeps the default no-op dispatcher (`hooksWired=false`), so `resolveHooksChain` silently returns an empty chain (the documented no-op path: no error, no log).
- `internal/server/router.go:593` — the only `WithHooks` injection site in the codebase — sits inside `if runner == nil` (router.go:521), a **fallback branch that no real deployment reaches** because the CLI always supplies the runner.

Everything around the gate works and is fully unit-tested: the dispatcher, matcher compilation, tier loading, command-handler execution, canonical block payload, audit rows. The gate simply is never wired into the serving runner. The hooks settings UI, validation, dry-run test endpoint, and audit trail all read/write the same stores — so a workspace can configure, verify, and audit hooks that will never run. This also poisons the harness benchmark's tool-error lane (its hook tasks measure recovery from blocks that never happen) and silently disables instance-level builtin hooks (policy gates shipped as "managed" hooks) for every workspace.

## What Changes

- **Wire the hooks dispatcher into the composition-root runner**: `internal/cli/server.go` gains `agents.WithHooks(agenthooks.NewDispatcher(st.Hooks(), hookRegistry))` on its `NewRunner` call, with the shared handler registry (command kill switch, MCP invoker, evaluator factory) built once next to it — mirroring what router.go:593 always intended.
- **Delete the router's duplicate fallback runner construction** (`internal/server/router.go` `if runner == nil` block, ~553–620): `RuntimeOptions.Runner` becomes a required, non-nil dependency per the house rule that injected dependencies are never nil and the composition root resolves every dependency. The drift between the two option lists is the defect class that caused this; one construction site removes the class. Server tests that relied on the fallback get the runner from a test composition helper instead.
- **Failing-test-first regression pin**: an integration test that boots through the real composition path, stores a workspace `pre_tool_use` command hook, drives a `/v1` turn that calls the matched tool, and asserts the block lands (canonical `blocked_by_hook` payload as the tool result) **and** a hooks-execution audit row exists. This test fails on today's tree.
- **Smoke coverage**: a smoke section driving the same shape end-to-end against a live server (hook create → turn → blocked tool result), so the no-op failure mode is visible from the outside.
- **Bench (nested module, no onclaw imports)**: nothing changes in `bench/` — its hook-block tasks and graders were **correct**; the harness under test was broken. The post-fix bench cycle measures recovery with the pre-registered noise-derived gates already committed in `bench/baselines/8565453-1.json`.
- No schema migration, no API change, no handler/dispatcher semantic change — the entire fix is wiring plus regression coverage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-hooks`: adds a **runtime wiring** requirement — the serving runner's hook chain MUST resolve from the wired dispatcher (never the silent no-op), proven by a live-run scenario: a stored workspace `pre_tool_use` hook gates a real turn with the canonical block payload as the tool result and an execution audit row. Existing requirements are unchanged; this pins the failure mode the tree shipped (stored, validatable, auditable hooks that never execute).

## Impact

- **Backend:** `internal/cli/server.go` (wire `WithHooks` + shared registry; expose the runner through `RuntimeOptions` unchanged), `internal/server/router.go` (delete the fallback builder; require `RuntimeOptions.Runner`), server tests bootstrapping without a runner.
- **Bench:** none (observer — the next cycle measures the repaired harness against `8565453-1.json` gates; tool-error lane expected to move well past its 0.202 noise floor as the 9 hard-fail hook trials become real recovery measurements).
- **Ops:** after deploy, stored hooks start enforcing **for the first time** — workspaces with hooks configured during the no-op era will see them fire; release note required.
- **Verification:** new integration test red-on-old/green-on-new; `go build/vet/test`; integration suite; smoke suite gains the hook-gate section; manual bench-pane pass on a hook-blocked session showing `blocked_by_hook` in the tool card.
