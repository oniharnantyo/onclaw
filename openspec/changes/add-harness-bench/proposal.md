# Proposal

## Why

OnClaw's agent harness (`internal/agents/` + the composed context it ships to the model) is the product's core, but harness changes today ship with only regression coverage (smoke.sh, web tests) — nothing measures whether an agent actually performs *better* after a harness change. The 2026 harness literature (locked-harness protocol 2605.23950, "Same Model, Different Harness" 2608.26218, ABC 2507.02825, SWE-agent ACI 2405.15793) shows harness deltas on a frozen model are among the largest quality levers, and that they are only provable with a pinned, repeatable benchmark. The end goal is a whole-harness enhancement program where every harness delta is bench-gated before and after; the benchmark must be **decoupled from the code it measures** — it lives in this repo but as a self-contained nested Go module that cannot import onclaw packages — so it stays independent of what it measures and can compare across OnClaw versions.

## What Changes

- New **nested, decoupled Go module** `bench/` at this repo's root, holding the benchmark: task suite, runner, scoreboard, baseline store. Own `go.mod` (module `github.com/oniharnantyo/onclaw-bench`) makes decoupling compiler-enforced: cross-module imports of `internal/` are impossible in either direction, and the directory can be extracted to a standalone repo later with no path changes.
- Two-tier benchmark, decided during exploration:
  - **Primary (every cycle):** purpose-built suite, ~30 tasks across 5 lanes mapped to harness surfaces — compaction/context, tool-error recovery, instruction fidelity, delegation (subagents/background), memory (patterned on the existing `eval-memory` protocol).
  - **Anchor (per milestone):** a handful of adapted Terminal-Bench (arXiv 2601.11868) tasks with hidden verifiers, guarding against overfitting to a self-written suite.
- **Locked-harness protocol:** one harness delta per cycle; pinned model (GLM-5.3-flash via Z.AI — same as the memory eval); ≥3 seeds; deterministic hidden graders first, rubric-pinned LLM judge only where necessary; pre-registered pass gates (the `agent-memory-eval` multihop-gate precedent); scoreboard JSON committed per run and diffed against the previous baseline.
- Program phases captured for traceability (delivered as separate future changes): Phase 1 single-delta gated harness cycles (compaction/context first, then tool loop, then composer); Phase 2 adaptive layers (GEPA 2507.19457 per-block prompt evolution, ACE 2510.04618 context deltas) using the bench as scorer; Phase 3 (self-closing loop — HarnessDev 2609.01437 family) parked, per prior decisions.
- **No changes to existing onclaw-v2 packages.** The bench drives OnClaw exclusively through its existing public HTTP API (the `eval-memory` pattern: fixture workspace + agent provisioning, admin login, transcript reads); root `go build ./...` and `go test ./...` are unaffected because nested modules are invisible to them.

## Capabilities

### New Capabilities

- None. The benchmark is standalone tooling in a nested module; OnClaw behavior is unchanged. External drivability already exists and is covered by the public API capabilities (`agents`, `agent-runtime`, chat/session surfaces).

### Modified Capabilities

- None.

## Impact

- **onclaw-v2:** zero footprint on existing packages — no imports in either direction. New top-level `bench/` directory (nested Go module): suite fixtures, runner CLI, scoreboard/baseline storage, README with the protocol. Extractable to a standalone repo later with no code changes.
- **Live-model cost:** each bench cycle spends real tokens (live server + Z.AI key, like `eval-memory`). Suite size is the budget dial.
- **Phase 1+ changes** (future, separate): harness deltas inside `internal/agents/`, each gated by a bench run. Boundary: harness = `internal/agents/` + composed context; model frozen; IDENTITY/SOUL tiers remain human-owned.
