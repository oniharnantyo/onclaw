# Tasks

All work happens in `bench/` at this repo's root — a **nested, decoupled Go module** (design D1): own `go.mod`, zero imports from `github.com/oniharnantyo/onclaw`, invisible to root `go build ./...` and `go test ./...`.

## 1. Scaffold external repo

- [ ] 1.1 Create `bench/` at the repo root as a nested Go module (`go mod init github.com/oniharnantyo/onclaw-bench`) with a README stub pointing back to this change for motivation. Verify: `cd bench && go build ./...` succeeds; root `go build ./...` and `go test ./...` are unchanged; `go list -deps ./...` inside bench contains no `github.com/oniharnantyo/onclaw` package.
- [ ] 1.2 Define the scoreboard JSON schema and task-fixture format (per task: lane, setup, prompt script, deterministic verifier, seed policy) as Go types with unit tests for serialization round-trip. Verify: `go test ./...` green inside `bench/`.

## 2. Driver (port from internal/memory/eval)

- [ ] 2.1 Copy and adapt (never import — the module boundary forbids it) the HTTP client and fixture provisioning (admin login, fixture workspace + agent creation/reuse, chat send, transcript read) from `internal/memory/eval/client.go` and `fixture.go`. Verify: integration check against a live local onclaw server provisions the fixture workspace and agent via public API only.
- [ ] 2.2 Build the `run` CLI (`go run . run --base-url --suite --out --seeds`): tasks × seeds executed sequentially with bounded backoff, per-task retry budget, and `infra-error` classified distinctly from task failure (design Risks). Verify: dry-run over a 1-task stub suite emits a valid scoreboard JSON with suite version, git sha, and model pin recorded.
- [ ] 2.3 Build the `compare` CLI: diff a scoreboard against the stored baseline, print per-lane deltas against the pre-registered gate, exit 0 on gate PASS / 1 on FAIL. Verify: unit test with two fixture scoreboards produces the expected delta report and exit codes.

## 3. Suite authoring (~30 tasks, 5 lanes)

Each lane below: author the tasks, apply the ABC authoring lint (solvability without the agent, grader consistency, unintervention check — design D5), and confirm the lane loads and lints clean via a suite-validation unit test added in that group.

- [ ] 3.1 Compaction/context lane (7–8 tasks): long threads crossing `context_window`, post-compaction recall, `transcript.md` offload fidelity. Verify: lane suite file loads and passes the ABC lint checks.
- [ ] 3.2 Tool-error recovery lane (7–8 tasks): injected tool failures, `disabled_tools` denylist interactions, structured-error handling. Verify: lane loads and passes the lint.
- [ ] 3.3 Instruction-fidelity lane (5–6 tasks): multi-doc system-prompt adherence, slash commands, skills precedence; record per task whether the verifier is deterministic or pinned-rubric judged (design Open Question default: deterministic). Verify: lane loads and passes the lint.
- [ ] 3.4 Delegation lane (5 tasks): subagent and background-shell outcomes with final-state verifiers. Verify: lane loads and passes the lint.
- [ ] 3.5 Memory lane (5–6 tasks): LongMemEval-protocol tasks ported from the `eval-memory` fixture set where they transfer. Verify: lane loads and passes the lint.

## 4. Baseline-0 and protocol

- [ ] 4.1 Record baseline-0 on onclaw master: full suite × 3 seeds against a live server with the pinned model; commit the scoreboard under `baselines/<git-sha>-<suite-version>.json`. Verify: committed baseline contains per-lane aggregates and the pre-registered gate values.
- [ ] 4.2 Document the cycle protocol in README: one harness delta per cycle → one run → gate decision; seed policy; infra-error policy; gate pre-registration convention; when the Terminal-Bench anchor fires (milestones). Verify: a reviewer can execute a full cycle from README alone.

## 5. Anchor lane and end-to-end

- [ ] 5.1 Port 3–5 Terminal-Bench tasks (2601.11868) with hidden verifiers adapted to OnClaw's jailed-dir sandbox (design Open Question: which tasks port — resolve here). Verify: anchor lane runs through the same runner and passes the lint.
- [ ] 5.2 End-to-end dry cycle: run the full suite against an unchanged harness and `compare` against baseline-0. Verify: report shows zero delta and gate PASS, exercising runner → scoreboard → compare → gate end to end.
