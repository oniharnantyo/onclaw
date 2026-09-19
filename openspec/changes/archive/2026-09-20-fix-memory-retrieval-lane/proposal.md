# Proposal: fix-memory-retrieval-lane

## Why

The first live scoreboard run (eval-20260919-174856, `memory-eval` workspace, glm-5.3-flash via Z.AI) returned recall=0% with every answer a faithful "not in memory" — the ingestion pipeline stored 39 notes/36 events correctly, but the turn-time retrieval lane delivered nothing to the model. Prime suspect: the intent gate's hard-coded 1.5s fail-open budget cannot accommodate a remote side-call model's round-trip (a probe turn took 39s end-to-end), so the gate times out on every turn and prefetch never injects. The run also exposed two measurement-infrastructure defects that make the scoreboard untrustworthy until fixed: the harness's ingestion wait polls as admin (blind to owner-visible notes) and Langfuse tracing is dark (zero traces in 24h, so side-call latency claims cannot be verified). Wave-3 decisions (vectors, graph) are gated on eval numbers that are currently measuring plumbing, not memory quality.

## What Changes

- Intent-gate timeout becomes a configurable workspace memory-setting (`gate_budget_ms`, bounded range, default raised from the pinned 1500ms to 4000ms); fail-open semantics unchanged — a timeout or error still means "self-contained".
- Eval harness ingestion-wait polls the notes API as the fixture owner instead of admin, so it observes notes landing and only waits as long as actually needed; the misleading `_seed` idle-pipeline note is corrected accordingly.
- Langfuse tracing is repaired to actually export (diagnose keys/sample-rate/callback wiring against the configured cloud project; acceptance = traces visible within minutes of a live turn). This is a conformance repair against the existing `langfuse-tracing` spec — no requirement changes.
- The scoreboard is rerun on the fixed lane and recorded in the change folder; only those numbers feed the wave-3 (vectors / entity-graph) go/no-go.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-memory-retrieval`: the Intent gate requirement gains a configurable per-workspace budget (settings-backed, bounded, raised default) while keeping hard-timeout fail-open semantics.

## Impact

- `internal/memory/intent.go` (budget plumbing), `internal/memory` settings record + validation (`gate_budget_ms` field), `internal/agents/runner.go` `composeMemoryDocs` (pass the resolved budget), settings HTTP handlers (field exposure + validation).
- `internal/memory/eval/seed.go` + `harness.go` (wait identity + `_seed` note text).
- Langfuse wiring in the composition root / callbacks (repair only; `langfuse-tracing` spec already requires the behavior).
- No wire-breaking changes; settings record gains one optional field (absence = default, consistent with the existing record's absence-is-defaults rule).
