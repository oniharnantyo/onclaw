# Design: fix-memory-retrieval-lane

## Context

First live scoreboard (eval-20260919-174856) evidence, all disk/live-verified 2026-09-19:

- Ingestion is healthy: 39 notes + 36 events in Postgres from the eval corpus; an explicit "remember this" probe produced a note with importance 8 and an `x.memory_ingested` chip; a lexical search for the probe word from the owning identity matched instantly.
- All 10 question answers showed zero fact knowledge; 7/10 opened no evidence, 2 opened evidence yet answered "not in memory". The intent-gate prefetch never injected. The gate runs with a hard-coded 1.5s context deadline (`internal/memory/intent.go`, design P2 of `integrate-agent-zero-memory`); the eval's side-call model is `glm-5.3-flash` via `api.z.ai` — a full probe turn took 39s, making a <1.5s remote round-trip implausible. Fail-open then silently degrades every turn to documents-only, and the model rarely self-searches.
- The harness's `waitForIngestion` polls the notes API with the ADMIN token; the notes API filters to the caller's visible set (shared + own-user + serving-agent), and admin holds no membership rows in the fixture workspace — so the wait is structurally blind and always burns its full 180s while believing "0 notes (pipeline may be idle)".
- Langfuse (us.cloud.langfuse.com project from `.env`) received zero traces in 24h, including plain model-request traces — so side-call latency could not be observed directly; the gate-timeout diagnosis is inferred from behavior. Root cause of the silence not yet identified (keys vs sample rate vs callback wiring).

Constraints carried over: the gate must stay fail-open and cheap (P2's polarity: recall insurance, never a correctness dependency); settings records follow absence-is-defaults; no defensive nil guards on injected dependencies.

## Goals / Non-Goals

Goals:
- Turn-time retrieval actually delivers on remote-model deployments: budget configurable, default survivable.
- The eval harness observes reality: wait ends when notes land, reports truthfully.
- Tracing flows again so the next scoreboard's latency story is provable, not inferred.
- A clean post-fix scoreboard that can legitimately gate wave-3.

Non-Goals:
- Changing gate semantics (still fail-open, still cheap-tier).
- Switching the gate to a local model runtime, embedding work, or any wave-3 scope.
- Re-scoring the old run; the baseline stays dropped (2026-09-19 decision).

## Decisions

- **D1: Budget lives in the memory settings record as `gate_budget_ms` (int, optional).** Absence = 4000ms default; validation bounds [500, 20000] with a 422 naming the range (mirrors the settings record's existing save-time validation style). Rationale: the workspace already owns side-call posture in this record; a per-workspace knob matches "remote vs local provider" being a workspace-level fact. Alternative considered: a global env knob (rejected — posture is per-workspace; env loses multi-tenant shape) and a per-agent override (rejected — the gate is a workspace-lane cost lever, not an agent persona trait).
- **D2: Default raised 1500ms → 4000ms.** Rationale: 1.5s fit a fast-lane assumption; observed remote reality (39s turns) makes even 4s tight, and the gate only pays it on non-trivial turns while still failing open. Alternative considered: keeping 1.5s and mandating a local gate model (rejected — imposes an ops requirement the eval deployment itself cannot meet today).
- **D3: The runner resolves the budget once per turn from the settings record and hands it to the IntentGate call** (the gate keeps its constructor shape; the deadline is a call-site value). Rationale: settings can change between turns without rebuilding the runner; keeps the existing side-call resolution chain (agent override → settings → agent's own model) untouched.
- **D4: Harness waits as the fixture owner.** `waitForIngestion` polls the notes API with Sari's token (owner, sees shared + own-user rows — the corpus's dominant tiers) instead of admin; the wait ends early when the expected minimum count appears, and the `_seed` note no longer claims "pipeline may be idle" based on an admin-shaped blind spot. Rationale: the notes API is the contract surface the pipeline exposes; polling through it (as a real principal) doubles as an implicit visibility regression check. Alternative considered: counting rows via direct DB access from the harness (rejected — the harness deliberately exercises only HTTP surfaces).
- **D5: Langfuse repair is diagnose-then-fix, not blind rewiring.** Steps: verify the `.env` keys against the configured project via the public API (a 401/404 names the problem); check `ONCLAW_LANGFUSE_SAMPLE_RATE` isn't 0; confirm the eino Langfuse callback handler is constructed and registered on the runner's callbacks in the running build. Acceptance: any live turn produces a visible trace within ~1 minute. No spec delta — `langfuse-tracing` already requires exactly this; this is conformance repair.
- **D6: Scoreboard rerun is part of this change, not an afterthought.** Fresh run id on the reused fixture workspace (re-seed is deterministic), scoreboard JSON recorded in the change folder next to the tasks that produced it. Wave-3 (pgvector, entity graph) remains gated on THOSE numbers.

## Risks / Trade-offs

- [4s default still too tight for very slow providers] → the knob exists per-workspace; the rerun uses Z.AI and will show whether 4s suffices there; fail-open keeps worst-case behavior identical to today, just less frequent.
- [Raising the default adds latency to gate-primed turns that would have failed open before] → bounded by the budget itself (≤4s on non-trivial turns only), and a successful gate replaces blind document-only composition with targeted candidates — the spend is the feature.
- [`gate_budget_ms` misuse (set to the 20s ceiling) lengthens every gate-primed turn] → bounds + it is an Owner/Admin-managed workspace setting, same trust tier as the side-call model choice.
- [Harness wait fix could mask a genuinely idle pipeline] → the early-exit only shortens the wait when notes ARE appearing; a silent pipeline still waits the full budget and now reports that accurately under an identity that can actually see the store.
- [Langfuse root cause may be upstream (revoked/rotated cloud keys)] → the spike names the failing layer before any wiring change; if keys are dead, the fix is a config/secret rotation task for the operator, and this change records that outcome instead of code.

## Migration Plan

1. Land the settings field + validation + gate plumbing (no behavior change until a workspace sets the field; default shift 1.5s→4s is the only immediate effect and is fail-open-safe).
2. Land the harness fix; rerun the scoreboard on the live fixture workspace; store the JSON in the change folder.
3. Langfuse repair rides independently — diagnose first; acceptance is a visible trace.
4. Rollback: revert is trivial per piece; the settings field is additive and ignored by older readers; no data migration.
