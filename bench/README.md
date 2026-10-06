# onclaw-bench

Standalone benchmark for the OnClaw agent harness. It measures harness quality as **agent task performance**: a versioned, purpose-built task suite is driven through a running OnClaw server's public HTTP API, every task is verified against deterministic (or pinned-rubric judged) checks, and the resulting scoreboard is gated against committed baselines with pre-registered gates.

It lives as a **nested, decoupled Go module** (`module github.com/oniharnantyo/onclaw-bench`, stdlib only) so the decoupling is compiler-enforced: it cannot import `github.com/oniharnantyo/onclaw`, the root module's `go build ./...` / `go test ./...` ignore it entirely, and the directory stays extractable. The bench talks to any OnClaw server version black-box over HTTP — endpoint drift surfaces as a bench failure, never as silent coupling. See `openspec/changes/add-harness-bench/` in the onclaw-v2 repository for the motivation and design decisions (D1–D8).

## Packages

- `suite/` — the task-fixture format (JSON schema for suites, tasks, setup steps, verification checks), strict loading, semantic validation, and the ABC authoring lint (solvability without the agent, grader consistency, unintervention).
- `scoreboard/` — the scoreboard JSON schema, per-lane aggregation (infra-errors excluded from the denominators), and the baseline + pre-registered-gate `Compare`.
- `driver/` — the black-box HTTP surface: admin login, fixture workspace/actor/agent provisioning (create-or-reuse, idempotent), chat turns over `POST /v1/responses`, transcript extraction, hook/connection/agent-patch setup calls.
- `runner/` — execution policy: suite loading + lint gate, the local control server for deterministic tool blocks, per-task setup/turns/checks, infra-error retry classification, dry-run, scoreboard assembly.
- `suites/` — the suite files, one JSON per lane. **Underscore-prefix convention:** files starting with `_` (e.g. `_stub.json`) are helper suites excluded from directory loads; run them explicitly by path.

## The cycle protocol (one harness delta per cycle)

The bench exists to gate harness changes. The protocol is **locked-harness** (design D4): the model is frozen, one harness delta is measured per cycle, and the pass/fail gate is **pre-registered before the after-run**.

1. **Before the change.** With the harness frozen at the current baseline:
   - **Purge the fixture workspace's learned memory** (`scripts/purge-memory.sh`) and verify the zeros. The memory pipeline re-learns during runs (turn-end ingestion), so memory state at cycle start depends on run history — cycles that start with different memory states are not comparable (the suite-v2 cycle measured identical prompts flipping 3/3-pass to 0/3-fail against the purged postfix cycle from accumulated memory alone). Never chat manually with the bench agent while a cycle runs, for the same reason.
   - Record the run: `go run . run ... --out /tmp/before.json` (full suite, ≥ 3 seeds, pinned model).
   - **Pre-register the gates** for the delta you are about to make — per lane, `max_regression` (how much pass rate the change may cost) and, for a change that claims improvement, `min_improvement` (how much it must gain). Gates live in the Baseline JSON's `gates` map and must be written and committed **before** the after-run. Copy the baseline artifact from `/tmp/before.json`, add the `gates` map, and commit it at `baselines/<git-sha>-<suite-version>.json`.
2. **Make the delta.** Exactly one harness change per cycle. Touching the model, the suite, or more than one harness surface invalidates the comparison.
3. **After the change.** Re-run with the same flags against the changed server: `go run . run ... --out /tmp/after.json`.
4. **Gate the delta.**
   ```
   go run . compare --run /tmp/after.json --baseline baselines/<git-sha>-<suite-version>.json
   ```
   Prints per-lane rows (baseline rate, run rate, delta) plus the overall row, each verdict PASS/FAIL against its registered gate. **Exit 0 = every gate passed, the delta lands. Exit 1 = a gate failed — the delta is rejected regardless of direction when the effect is inside the registered noise band.** Commit the after scoreboard next to the baseline if the delta lands.
5. **Suite-version bump + fresh baseline-0** are required whenever a change breaks the API the suite rides on (the smoke suite gates API breaks): bump every lane file's `suite_version`, re-record baseline-0 on the *unchanged* harness, and re-register gates.

### Seed policy

- **≥ 3 seeds per task**, run sequentially (`--seeds 3`). Task scores are reported as mean pass rate per lane with the run-to-run spread kept in mind: a delta inside the noise band must be rejected by its pre-registered gate (`min_improvement` at least the observed spread), no matter its direction.
- Seeds are pure repetition: each seed gets a fresh session id (`bench-<runid>-<task-id>-s<seed>`), same setup, same checks.
- **Gate calibration (measured, 2026-10-04):** with 3 seeds, a lane near 50% has a seed-noise σ of ~10pp (binomial, n=24) — the unchanged-harness dry cycle swung the tool-error lane −12.5pp (~1.2σ) and tripped a flat 0.10 gate. Gate floors must therefore be **per-lane and noise-derived**: `max_regression ≥ 2σ ≈ 2·sqrt(p(1−p)/trials)` for the lane's baseline rate, or the lane needs more seeds. Near-50% lanes at 3 seeds: use ≥ 0.20, not 0.10. High lanes (≥80%) tolerate 0.10. `8565453-1.json` carries those noise-derived floors (anchor 0.315, compaction 0.136, delegation 0.129, instruction-fidelity 0.149, memory 0.176, tool-error 0.202, overall 0.083 — ceil of 2σ at each lane's baseline rate); re-derive them whenever the baseline scoreboard or the seed count changes.

### Infra-error policy

Infrastructure failures (HTTP errors, network failures, 5xx, 429, client timeouts) during setup or turns are recorded as status **`infra-error`** — distinct from task failure — with the cause text, and are:

- **retried** up to `--retries` (default 2) with bounded exponential backoff (`--backoff`, base 5s, doubling, cap 60s; 429s back off the same way),
- **excluded from every pass-rate denominator** (a lane that half-infra'd is not scored down for it), and
- counted in the scoreboard's `infra_errors` tally — a run with a large infra tally is not a valid measurement; re-run it.

A `/v1` response whose run status is not `completed` (e.g. `failed`, `prompt_blocked`) is a recorded task **failure**, not infra — it is never retried.

### Gate pre-registration convention

- Gates are stored inside the committed Baseline JSON (`gates`: lane id → `{max_regression, min_improvement}`, plus `"overall"` for the cross-lane row) and are written **before** the after-run — the same discipline as the `agent-memory-eval` multihop gate.
- The default (unregistered) gate is zero-tolerance: any regression fails.
- Gates are part of the baseline commit: `baselines/<git-sha>-<suite-version>.json` carries both the before scoreboard and the gates the next cycle must pass.

### Anchor lane (Terminal-Bench port)

The `anchor` lane (adapted Terminal-Bench tasks with hidden verifiers) fires **at milestones only** — it exists to break self-suite circularity, not to be run every cycle. Include it when the protocol says a milestone is reached; the runner treats it like any other lane.

### Judge policy

Deterministic graders first (design D5): file-state, tool-card, and API-effect checks cover the default lanes. A `judge` check is a **lint failure unless the suite sets `judge_allowed: true`**. When a suite does opt in:

- Pin `ONCLAW_BENCH_JUDGE_KEY` (required — a judge task without it records infra-error with an actionable message), optionally `ONCLAW_BENCH_JUDGE_BASE_URL` (default `https://api.z.ai/api/paas/v4`, OpenAI-compatible `/chat/completions`), and `--judge-model` (default `glm-5.3-flash`).
- The judge model is **pinned separately from the agent-under-test model** so judge drift cannot masquerade as harness improvement; both pins ride in the scoreboard.
- The judge is prompted as a strict grader that replies exactly PASS or FAIL plus a one-line reason, and grades only the pinned rubric in the task fixture.

### The pinned model

The bench protocol pins the agent-under-test model to **GLM-5.3-flash via Z.AI** (`--model glm-5.3-flash`, provider base `https://api.z.ai/api/paas/v4`). Changing the model pin forces a fresh baseline-0 — a model change is a harness change for gating purposes. The provider key comes from the environment (`ONCLAW_BENCH_PROVIDER_KEY`), never from code.

## Prerequisites

- A running OnClaw server with a migrated PostgreSQL database (`go run . server` in the onclaw repo, `go run . migrate up` first).
- Admin credentials (`--email`/`--password`, or `ONCLAW_BENCH_ADMIN_EMAIL` / `ONCLAW_BENCH_ADMIN_PASSWORD`) — superadmin, or the owner of an existing fixture workspace.
- A workspace provider with live keys, or `ONCLAW_BENCH_PROVIDER_KEY` set for provider bootstrap (agent creation runs live prompt generation, so the live path needs working model keys).
- The bench must run on the **same filesystem as the server** (the documented local protocol): tool blocking registers workspace hooks with **command handlers**, and the harness executes a command hook's program on the server host — the runner materializes the script body to a temp file (`onclaw-bench-hook-*.sh`, chmod 0755) and registers it as `/bin/sh` exec-form config. Command handlers are enabled by default (`hooks-command-enabled`); the runner deletes the script — and the per-hook control file it creates for a `{{control_file}}` placeholder — at teardown.
- HTTP hook handlers remain schema-valid for harnesses that allow private hook targets: a handler URL carrying `{{control_url}}` is still pointed at the bench's local control server, which answers with the canonical decision JSON.
- **`--context-window` pin:** a run whose loaded suites override `context_window` in `agent_config` steps requires an explicit `--context-window <tokens>` pin — the agent PATCH cannot unset `context_window`, so without the pin the runner aborts before anything runs, listing the affected task ids. The pin is the baseline every after-task restore returns to.
- **Memory-lane ingestion wait:** the memory pipeline is async (ingestion → `memory.search`), so the memory lane's fact-seeding tasks carry a bounded `wait` setup step (120s) after their seeding turns — the in-repo eval precedent's bounded wait — so measured recall probes only run once ingestion has landed.

| Env var | Meaning |
| --- | --- |
| `ONCLAW_BENCH_ADMIN_EMAIL` | backfills `--email` |
| `ONCLAW_BENCH_ADMIN_PASSWORD` | backfills `--password` |
| `ONCLAW_BENCH_PROVIDER_KEY` | provider API key used only when the fixture workspace has no enabled provider |
| `ONCLAW_BENCH_JUDGE_KEY` | judge API key (only suites with `judge_allowed: true`) |
| `ONCLAW_BENCH_JUDGE_BASE_URL` | judge OpenAI-compatible base URL (default `https://api.z.ai/api/paas/v4`) |

## Quickstart

```bash
cd bench

# 0. Build and test the module (kept out of the root module's test suite on purpose)
go build ./... && go vet ./... && go test ./...

# 1. Lint + plan without spending a token: load suites, lint, emit a structural scoreboard
go run . run --suite suites --seeds 3 --dry-run --out /tmp/bench-dryrun.json

# 2. Live smoke with the stub (underscore-prefixed: explicit path, not part of directory loads)
go run . run --suite suites/_stub.json --seeds 1 \
  --base-url http://localhost:8080 --email admin@example.com --password ... \
  --model glm-5.3-flash --out /tmp/stub-scoreboard.json

# 3. Full run (all lane files under suites/, 3 sequential seeds)
go run . run --base-url http://localhost:8080 --email admin@example.com --password ... \
  --workspace bench-eval --agent bench-agent --model glm-5.3-flash \
  --suite suites --out scoreboard.json --seeds 3

# 4. Gate the delta against the committed baseline
go run . compare --run scoreboard.json --baseline baselines/<git-sha>-1.json
```

## Flag reference

### `run`

| Flag | Default | Meaning |
| --- | --- | --- |
| `--base-url` | `http://localhost:8080` | base URL of the running OnClaw server |
| `--email` / `--password` | env backfill | admin identity (provisions the fixture) |
| `--workspace` | `bench-eval` | fixture workspace slug (created when absent, reused when present) |
| `--agent` | `bench-agent` | fixture agent slug (created when absent, reused + repointed when present) |
| `--model` | `glm-5.3-flash` | the pinned agent-under-test model (the scoreboard's model pin) |
| `--context-window` | `0` | pin the agent's context window for the whole run (0 = server default). **Required when any loaded suite overrides `context_window`** (`agent_config` step): the runner aborts without the pin, because the public API cannot clear an override — the pin is what task restore returns to |
| `--suite` | `bench/suites` | suite file or directory (directory loads skip `_`-prefixed helpers) |
| `--out` | `scoreboard.json` | scoreboard JSON path |
| `--seeds` | `3` | sequential seeds per task |
| `--retries` | `2` | per-(task,seed) infra retry budget |
| `--backoff` | `5s` | exponential backoff base (cap 60s) |
| `--judge-model` | `glm-5.3-flash` | judge check model, pinned separately from the agent |
| `--dry-run` | off | load + lint + plan only; emits a structural scoreboard where every row is a recorded fail (`dry-run (no model call)`), with suite version, git sha, and model pin still recorded. Prints a loud `DRY RUN` banner |
| `--seed-only` | off | provision the fixture workspace/agent/actors and exit (no tasks) |
| `--score-only` | off | skip provisioning — the fixture must already exist; logs in, reads the agent (its stored model is the pin), and runs the scored tasks directly |

Exit codes: **0** when the scoreboard was written — task failures are data, the gate decides; **1** on operational failure (unreachable server, lint issues, suite-version mismatch); **2** on flag misuse.

### `compare`

| Flag | Meaning |
| --- | --- |
| `--run` | the run's scoreboard JSON |
| `--baseline` | the committed baseline JSON (before scoreboard + pre-registered gates) |

Prints per-lane rows (baseline rate, run rate, delta, verdict) plus the overall row. **Exit 0 iff every gate passes, else 1.**

## How a task executes

For every task × seed, sequentially: a fresh session id (`bench-<runid>-<task-id>-s<seed>`, retried attempts get `-r<attempt>`); setup steps in order — `chat_turns` seeded as the named fixture actor (`casey`/`riley`) in the named logical session scoped to the task+seed, `agent_config` patched onto the agent (and restored to the run's pinned baseline afterwards), `hook` registered as a workspace hook — http handlers get `{{control_url}}` substituted by the bench's control server, command handlers get their script body written to a temp script (with any `{{control_file}}` placeholder substituted) and are registered as `/bin/sh` exec form — both best-effort deleted afterwards along with the command hook's files, `connection` created (best-effort deleted), `wait` slept; then the measured turns through `POST /v1/responses`; then every check, AND-ed into the verdict.

The **control server** is how the bench blocks a tool deterministically through the http-handler variant: a local HTTP server on `127.0.0.1` (ephemeral port) serving `/block` and `/allow`, which return exactly the harness's canonical hook decision bodies (`{"decision":"block","reason":…}` / `{"decision":"allow","reason":…}`). A `pre_tool_use` hook pointing `{{control_url}}/block` at a tool makes the harness turn that tool call into the canonical blocked tool result. The suites' default tooling is the **command-handler** variant instead: the runner-written temp script blocks by exiting 2 with a message on stderr (the harness's command-hook decision table), so no network path is involved.

Checks match on normalized text (`suite.NormalizeText`: lowercase, apostrophe folding, backtick strip — the design contract has the model chip file/tool names in monospace, so probes are markup-blind — punctuation-to-space folding, whitespace collapse) and accept either the suite's dotted tool convention (`files.write`) or the wire tool name (`write_file`) for the known aliases. `transcript_file_write` matches the written path as a suffix of the tool call's path (jailed-dir mounts prefix absolute paths).

## Test

```
cd bench && go test ./...
```

Hermetic — no server, no model keys. The runner's tests drive a fake OnClaw server end to end; live-model behavior is only exercised by actually running the CLI against a live server.
