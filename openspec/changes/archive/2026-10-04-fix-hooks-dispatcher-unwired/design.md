# Design

## Context

Live-verified root cause (dev server, 2026-10-04; evidence in the proposal). One diagram of the drift:

```
internal/cli/server.go:551  NewRunner(...)            → Runner: runner (cli/server.go:727)
                             option list: NO WithHooks      ↓
                                                            RuntimeOptions.Runner (non-nil in prod)
                                                            ↓
internal/server/router.go:521  if runner == nil {        ← DEAD in production
                                 router.go:553 NewRunner(   ← the ONLY WithHooks site (:593)
                                   ...full option list)
                               }
```

The runner that serves every turn resolves hooks through `resolveHooksChain` (runner.go:1933): with `hooksWired=false` it returns `NewNoopDispatcher().Resolve(...)` — an **empty chain, silently** (no error, no log; the no-op path is by design for unit constructions). Downstream, `run()` skips the prompt gate, never calls `ObserveRunStarted`, and `composeAgent` attaches no hooks middleware (agent.go:554 requires `HasHooks()`). Everything a user can see (settings UI, dry-run test endpoint, executions audit API) reads the stores directly and reports healthy, enforcing hooks.

## Decisions

### D1 — One runner construction site; the composition root owns it

Delete the router's `if runner == nil` fallback builder wholesale. Per AGENTS.md ("the composition root resolves every dependency — code at the point of use assumes non-nil"), `RuntimeOptions.Runner` becomes a required dependency: the router takes it as given and every consumer (v1 handlers, channel runtime, scheduler) receives the composition-root-built runner. The fallback existed as a test convenience; tests move to a small test composition helper (or construct `agents.NewRunner` themselves) rather than dragging a second production option list along.

Rejected: merging the router's option list into the CLI builder (keeps two sites), or "just add WithHooks to the CLI list too" (leaves the drift in place — the next option added only to the router branch recreates this class of defect).

### D2 — Wire the dispatcher once, at the composition root

`internal/cli/server.go` builds the shared `agenthooks.NewRegistry(...)` (same construction as router.go:462: encryption key, command/script kill switches from config, MCP invoker, evaluator factory) and passes `agents.WithHooks(agenthooks.NewDispatcher(st.Hooks(), hookRegistry))` on its `NewRunner` call. The registry instance is also what the hook handlers keep using for the dry-run endpoint — one registry, one kill-switch source, unchanged semantics. The router's hookHandlers construction keeps receiving the same registry (now built in or passed through from the CLI runtime options; prefer building it in `internal/cli` and shipping it on `RuntimeOptions` so both sides share one value).

### D3 — Regression pin is behavioral, not structural

The failing-first test drives a real turn: boot via the production composition path, `POST` a workspace `pre_tool_use` command hook (a script that exits 2 with stderr), send a `/v1` turn that calls the matched tool, and assert (a) the tool result in the transcript/output carries `"blocked_by_hook":true`, (b) the wrapped tool's endpoint never executed (file-side effect absent), and (c) `GET /hooks/executions` records the delivery. A unit test asserting "WithHooks is present in some option list" would not have caught this and is explicitly not the gate.

### D4 — Hooks semantics unchanged; the era of silently-armed hooks ends loudly

No dispatcher, matcher, tier, or payload changes. Two consequences are accepted and called out:

1. **Release note (breaking-by-fix):** hooks configured while the gate was a no-op begin enforcing on deploy. Instance builtin hooks (policy gates marked managed) also start evaluating. The change ships with a changelog entry stating this explicitly.
2. **Bench baseline continuity:** `bench/baselines/8565453-1.json` gates were calibrated on a harness whose hook tasks hard-fail (9 of 24 tool-error trials). The fix is exactly the kind of delta the cycle protocol exists for: one post-fix full run, compared with the **pre-registered** noise-derived gates (tool-error floor 0.202 regression / any-lane 2σ). An improvement inside the noise band is inconclusive; the expected signature is tool-error clearing its band while other lanes hold.

### D5 — Observed contaminants recorded, not chased here

During live reproduction the bench agent cited a "session filing rule" from **agent memory** to reroute a write into `/workspace/reports/` against the task's literal path, and the probe answers carried `[ATLAS]`-style markers. Both are memory/prompt contamination of the fixture agent, not hook behavior; they are recorded as bench-fixture hygiene (purge bench-agent memory before the post-fix cycle) but are out of scope for this change.

## Risks

- **Runner construction removal breaks hidden consumers** — the fallback branch may be load-bearing in tests. Mitigation: `go build ./... && go test ./...` plus the integration suite; any test that booted with `Runner: nil` switches to the composition helper in the same commit.
- **Post-deploy enforcement surprises** — mitigated by the release note (D4) and by the fact that hooks were configurable-but-inert, so the population of affected workspaces is discoverable from the stores before deploy.
- **Command-handler kill switch** now actually gates production behavior (`DefaultHooksCommandEnabled=true`); operators disabling it get failing-closed policy absence exactly as documented — the dry-run endpoint already reports the kill-switch state.
