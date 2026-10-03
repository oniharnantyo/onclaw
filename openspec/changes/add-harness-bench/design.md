# Design

## Context

The harness under measurement is `internal/agents/` plus the composed context it ships to the model (see `docs/agent-runtime-architecture.md`): instruction composer (AGENTS/IDENTITY/SOUL/WORKSPACE/USER/BOOTSTRAP), skills resolver, context-window-triggered compaction with `transcript.md` offload, the Eino ReAct middleware chain (patchtoolcalls → reduction → summarization → skill → filesystem), tool registry denylist, hooks/gates, memory gate/gister, subagents + background shell. Today its only automated coverage is regression-shaped: smoke.sh (API assertions) and web tests — neither scores agent task quality.

The in-repo precedent this design reuses is the memory eval harness (`internal/cli/eval_memory.go` + `internal/memory/eval/`): fixture workspace/agent provisioning over the public API, `--seed-only`/`--score-only` split, scoreboard JSON output, and an A/B decision test. Per the proposal, that pattern is cloned into a **nested, decoupled Go module** in this repo rather than extended in place.

Prior decisions this builds on: model pin GLM-5.3-flash via Z.AI (the eval-memory live precedent), playbook/self-closing-loop declines (Phase 3 stays parked), skills + memory lanes already shipped and evaluated.

## Goals / Non-Goals

**Goals:**
- A standalone benchmark repo that measures OnClaw harness quality as agent task performance, drivable against any OnClaw server version without recompilation against its internals.
- Per-surface scoring: lane-level scores attribute quality changes to harness surfaces.
- A repeatable before/after protocol rigorous enough to gate Phase 1+ harness deltas (locked-harness, pre-registered gates, committed baselines).

**Non-Goals:**
- No onclaw-v2 code changes in this change; no new OnClaw API surface (black-box HTTP only).
- No model training/fine-tuning; the model is frozen by protocol.
- No IDENTITY/SOUL content edits (human-owned tiers).
- No autonomous harness-evolution loop (Phase 3 parked).
- No CI integration in this change (bench runs are invoked manually per cycle; wiring into CI is a later decision once cost is known).

## Decisions

**D1 — Benchmark lives in-repo as a nested, decoupled Go module (`bench/` at the repo root), not coupled to the onclaw module.**
Its own `go.mod` (module `github.com/oniharnantyo/onclaw-bench`) makes decoupling compiler-enforced: Go's `internal/` visibility rule blocks cross-module imports in both directions, so the bench cannot reach `internal/agents`, and root `go build ./...` / `go test ./...` ignore nested modules entirely. The directory stays extractable — move it to a standalone repo, keep the module path, nothing breaks. Alternatives considered: an external sibling repo (the original D1) — user refined to in-repo placement with the same decoupling requirement; a same-module package tree under `bench/` — rejected, because imports of `internal/` would still compile (coupling would be convention-only) and root build/test/vet would sweep the bench into the main project's surface.

**D2 — Two-tier benchmark: purpose-built suite (primary) + adapted Terminal-Bench tasks (anchor).**
No public benchmark exercises OnClaw's actual surfaces (composer output, compaction trigger, memory prefetch, delegation), so the primary suite is purpose-built (~30 tasks, 5 lanes). A self-written suite alone is circular — ABC (2507.02825) and HarnessDev (2609.01437) both document self-referential eval failure modes — so a small set of Terminal-Bench (2601.11868) tasks with their hidden verifiers is adapted as a sixth, milestone-only lane. Alternatives rejected: adopting any single external benchmark wholesale (SWE-bench wrong domain; GAIA/τ²-bench measure the agent on external domains, blind to harness internals; Terminal-Bench's Harbor driver replaces the harness rather than inspecting it).

**D3 — Black-box HTTP-only driver.**
The bench talks to a running OnClaw server exclusively through the public API (auth, workspace/agent provisioning, chat, transcript reads), cloning the `internal/memory/eval` client/fixture patterns into the external repo. Alternative rejected: an in-process Go harness (importing `internal/agents`) — deterministic but couples the bench to internals; the nested module boundary (D1) makes that import impossible to express, which is the point.

**D4 — Locked-harness protocol.**
One harness delta per cycle; frozen model (GLM-5.3-flash via Z.AI) pinned in every scoreboard; ≥3 seeds per task; scores reported as mean + spread; pass/fail gates on a delta are **pre-registered before the after-run** (the `agent-memory-eval` multihop-gate precedent); every scoreboard is committed under `baselines/` keyed by OnClaw git sha + suite version. If the program later automates, add a PACE (2606.08106)-style anytime-valid acceptor rather than naive threshold checks. Alternatives rejected: ad-hoc before/after eyeballing (p-hacking surface); factorial model×harness sweeps (unnecessary while the model is frozen by protocol).

**D5 — Graders: deterministic first, LLM judge only where unavoidable.**
File-state and API-effect assertions are the default (Terminal-Bench hidden-verifier style; ABC whole-string/substring guidance). Where a task needs judged output (instruction fidelity), the rubric is pinned in the task fixture and the judge model is pinned separately from the agent-under-test model, so judge drift can't masquerade as harness improvement.

**D6 — Suite composition (~30 tasks, budget dial).**
Lanes mapped to harness surfaces: compaction/context (7–8 tasks — long threads crossing `context_window`, post-compaction recall, `transcript.md` offload fidelity), tool-error recovery (7–8 — injected tool failures, denylist interactions, structured-error handling), instruction fidelity (5–6 — multi-doc system prompt adherence, slash commands, skills precedence), delegation (5 — subagent + background shell outcomes), memory (5–6 — LongMemEval-protocol tasks reused where they transfer from `eval-memory`), Terminal-Bench anchor (3–5, milestone only). Suite size is the cost dial; lanes score independently.

**D7 — Bench repo stack: Go module, minimal dependencies.**
Reuses the team's Go fluency and lets `client.go`/`fixture.go` port near-verbatim from `internal/memory/eval` (copied source, never imported — D1 makes the import impossible). Plain flag-based CLI (`go run . run --base-url … --suite … --out …`), no framework. Alternatives rejected: Python/TypeScript — no precedent benefit, slower port of the client/fixture logic.

**D8 — Scoreboard format.**
JSON per run: suite version, OnClaw git sha, model pin, seed count, per-task verdicts (grader-kind, per-seed results, latency, token usage where exposed), per-lane aggregates, overall. Baselines committed in-repo keyed by sha; a `compare` subcommand diffs the latest run against the stored baseline and prints the per-lane delta against the pre-registered gate.

## Risks / Trade-offs

- [Run-to-run model variance swamps small harness deltas] → ≥3 seeds, mean + spread reporting, pre-registered minimum-effect gates; if a delta's effect is inside noise, it is rejected regardless of direction.
- [Live-token cost per cycle] → suite sized ~30 tasks; anchor lane only at milestones; `--seed-only`/`--score-only` split carried over so suites can be provisioned once and rescored.
- [Provider rate limits / flaky side-calls (Z.AI)] → seeds run sequentially with bounded backoff and a per-task retry budget (the memory side-call incident lesson); a task erroring on infrastructure is recorded as `infra-error` and excluded, never as a task failure.
- [Bench drift as the OnClaw API evolves] → suite version rides in every scoreboard; the smoke suite already gates API breaks; breaking API changes require a suite-version bump and a fresh baseline-0.
- [Accidental coupling as the bench evolves] → compiler-enforced by the nested module boundary (D1); the README protocol pins the black-box contract so endpoint drift surfaces as a bench failure, not silent coupling.
- [Bench tests don't run under root `go test ./...`] → intentional: keeps live-model tests out of the default suite; run `cd bench && go test ./...` explicitly, documented in README.
- [Self-suite circularity] → Terminal-Bench anchor lane at milestones; ABC checklist applied as an authoring lint (solvability without the agent, grader consistency, "unintervention" check).
- [Harness deltas tuned to weak-model quirks don't transfer] → acceptable by protocol (model is frozen); revisit only if the pinned model changes, which forces a fresh baseline.

## Migration Plan

1. Scaffold `bench/` in this repo as a nested Go module (`go mod init github.com/oniharnantyo/onclaw-bench`, README carrying this protocol); confirm root `go build ./...` and `go test ./...` are unaffected.
2. Port client/fixture from `internal/memory/eval`; build runner + scoreboard + compare.
3. Author the 5 internal lanes (~30 tasks); apply the ABC lint.
4. Record baseline-0 on current onclaw master (3 seeds) and commit it.
5. Phase 1 begins as separate onclaw-v2 changes (compaction/context delta first); each ends with a bench cycle that must pass its pre-registered gate.

Rollback: the bench is read-only with respect to OnClaw and imported by no existing package — delete or extract `bench/` and nothing else changes.

## Open Questions

- Which Terminal-Bench tasks port cleanly to the jailed-dir sandbox (no network dependencies, deterministic verifiers) — resolve during implementation lane 6.
- Whether any instruction-fidelity task genuinely needs the LLM judge, or all five lanes can stay deterministic — resolve during task authoring; default is deterministic.
