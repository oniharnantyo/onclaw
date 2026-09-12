# Design: integrate-scheduler

## Context

The event model already reserves a non-user run origin (`internal/agents/events.go` — `OriginCron`, never emitted), hooks already carry origin on every event, and `internal/channels/fanout.go` proves the full pattern this change mirrors: deterministic session id → attributed `ExecRequest` → async `RunSubmitter.Run` → drain goroutine writing the outcome back. The web ships a client-mock schedules screen (`CronView`, `CronEditorModal`, `CronChip`) whose data model (`CronJob`: name, agent, expr, human, next, enabled, last {status, when, dur}) is the UI contract's vocabulary. There is no run-records store anywhere — run identity today is (session_id, turn_id) in `session_events` — and no retention machinery exists in the codebase; the `agent_sessions` delete path is deliberately soft. Workspace timezone is IANA-validated at the domain layer.

See proposal.md for motivation and scope; specs/ for the behavior contract.

## Goals / Non-Goals

**Goals:**

- A multi-instance-safe, DB-backed scheduler loop with correct catch-up (fire once, reschedule, never replay).
- Scheduler runs indistinguishable from first-class runs: real sessions, real transcripts, hooks honored, usage metered.
- Unattended-run economics: preflight gating so a broken scheduler never spends tokens.
- Two creation surfaces (dashboard friendly-builder, `schedule` agent tool) sharing one validator.
- The web schedules screen moves from mock to live API.

**Non-Goals:**

- Origin-chat delivery (needs a synthetic session-notice writer; deferred with its own mockup approval).
- External message-channel targets (Telegram et al.).
- Run retention / deletion (growth documented below; first destructive path deliberately not built here).
- Failure alerting, failure streaks, auto-disable-after-N, incidents/doctor (Hermes/OpenClaw-class operational tooling).
- Dynamic cadence (pacing), condition watchers, stream/on-exit triggers, agent-facing scheduler self-management opt-ins.

## Decisions

**D1 — Terminology and the origin rename.** The feature is "scheduler" everywhere: UI copy, API paths, domain types, permissions, the screen. The reserved origin value renames `cron` → `scheduler` (constant `OriginScheduler`) now because zero consumers exist; the agent-hooks spec's value set changes with it. After any hook consumer ships, this rename becomes a compatibility burden. Alternative considered: keep the wire value `cron`, name the feature scheduler — rejected as a permanent mismatch for a one-time free rename.

**D2 — Canonical schedule: 5-field cron in the workspace timezone, one-shots as a sibling kind.** `schedulers.expr` stores a standard 5-field expression; `kind` is `recurring` | `once` with `run_at` for once. The friendly builder (presets, time picker, day chips) *generates* expressions; custom mode exposes the raw field with a next-runs preview. `robfig/cron/v3` (standard parser, `Next()` computation) is the only new dependency; it implements Vixie DOM/DOW OR semantics, which only custom mode can reach. The human-readable label ("09:00 · Mon–Fri") is derived at read time, never stored from input. The mock's `human` editor field disappears — the editor composes the schedule; the server derives the label. Timezone is the workspace's (already IANA-validated); no per-scheduler tz. Alternative: store structured recurrence JSON — rejected; cron is the interoperable canonical form and the builder makes it user-friendly, which was the actual requirement.

**D3 — Claim semantics: `UPDATE … WHERE id IN (SELECT … FOR UPDATE SKIP LOCKED …) RETURNING`.** One statement claims up to N due schedulers and writes each row's *next* occurrence (pre-computed to be in the future) at claim time. This single choice yields: no double-fire across ticks or server instances (row locks); correct restart catch-up (an overdue row is still `next_run_at <= now()` at startup, fires once, next lands in the future); no replay storms. A once-scheduler past a grace window (1h, constant) on claim is marked `missed` and archived instead of fired. Claim uses DB `now()`, not app clocks. Tick cadence 15s (`ONCLAW_SCHEDULER_TICK`); per-run wall-clock budget 10m (`ONCLAW_SCHEDULER_RUN_TIMEOUT`); global concurrency ~4 (semaphore). Alternatives: a `running` state column cleared on completion (GoClaw) — rejected; it turns every crash into recovery logic, while claim-time rescheduling is crash-safe by construction. A 1s tick (GoClaw) — rejected; minute-granularity cron makes 15s latency invisible and 1s buys nothing.

**D4 — The fire path mirrors the channels chokepoint.** New `internal/scheduler` package: `Service` (ticker + claim + dispatch), `drain` (event-tap consumer). `fireJob` does preflight (creator live-resolved via `UserStore`; agent config resolved; failure → `blocked`, no model call), then builds `ExecRequest{Origin: OriginScheduler, SessionID: "sched_<schedulerID>_<UTC timestamp>", UserID: creator, Input: prompt}` and submits via `RunSubmitter` (satisfied by `*agents.Runner`). The drain tallies tool calls, captures the final assistant text, duration, tokens, turn id, terminal status — then writes `last_run` and performs delivery. No late-binding cycle: the runner never needs the scheduler service (the `schedule` tool touches the store, not the service), so the composition root wires it plainly after the runner, unlike channels' `BindRunner` dance. In-flight schedulers are tracked in an in-memory map for the 409 rule (D9).

**D5 — Acting identity: the creator, resolved at fire time.** `created_by` is `ON DELETE SET NULL` (house precedent: `agents.created_by`); the fire path treats missing *or disabled* creator as `blocked` + auto-pause. This keeps permissions current with the creator's role and gives a clean story for "why does this agent have those permissions" — it inherited the creator's. Alternative: a workspace service identity — rejected; silent privileged execution is unanswerable in a multi-tenant product, and "scheduler dies with its creator's account" is visible, pausable behavior, not a surprise. Agent deletion cascades the scheduler (`ON DELETE CASCADE`, house precedent for every agent-attached object).

**D6 — Trimmed execution profile keyed on origin.** `composeAgent` branches on `req.Origin` (the channel-docs branch already exists). For `OriginScheduler`: compose AGENTS/IDENTITY/SOUL + workspace metadata doc *without* the shared-memory subsection, append the unattended contract (final reply is the deliverable; state failures plainly; `NO_REPLY` when nothing to report); omit USER.md, BOOTSTRAP.md, channel docs. Tool resolution additionally excludes the `schedule` tool and memory tools (anti-runaway: a scheduled run cannot mint schedulers — Hermes' rule — and cannot silently edit a human's memory). Everything else (hooks, summarization, usage, jail) applies unchanged. Interactive and channel runs compose byte-identically to today.

**D7 — Run sessions are artifacts, not chat history.** Run sessions persist transcript events (readable, linkable, hydrate like any session) but are **never written to `agent_sessions`** — the per-user index stays human-only and the sidebar never floods. The Runs listing queries transcripts via the deterministic `sched_<schedulerID>_` session-id prefix (indexed by prefix on the events table or a lightweight run-records view — implementation detail, see tasks). Each run is its own session per the user-locked isolation decision; RunManager's one-run-per-session guard therefore never collides, and the in-flight map (D4/D9) provides the per-scheduler exclusion instead. **Verify-first:** confirm the session-events read path resolves transcripts by session id without requiring an `agent_sessions` row; if it joins through the index, add a narrow exemption for `sched_` ids. **Checkpoints:** run sessions are never resumed; if the session adapter makes events-without-checkpoints a clean toggle, skip them (largest growth component, pure avoidance). If not separable, keep writing checkpoints — correctness first, growth documented (below).

**D8 — Delivery: thread default; channel via the chokepoint; `NO_REPLY` suppression.** `delivery` is `{"type":"thread"}` (default) or `{"type":"channel","channel_id":…}`. Channel delivery posts the final reply through the existing chokepoint `Post` as agent-author — it re-enters the normal pipeline (mention parsing can legitimately summon other agents). `NO_REPLY` (whole-word, case-insensitive, GoClaw's convention) suppresses the channel post; the run records `completed` with suppressed delivery. Channel-target validity (agent is a member) is checked at create/update; a target invalidated later records a delivery failure on the run — `completed` + `delivery_failed`, never a failed run (OpenClaw's split). Thread-target runs always "deliver" (the transcript is the product); their unattended contract asks for plain "Nothing to report." rather than the literal token, so transcripts never show `NO_REPLY`. Origin-chat delivery is explicitly deferred (no writer appends agent-authored entries into an existing session; needs design + mockup).

**D9 — Run-now and lifecycle.** Run-now is a direct dispatch (bypasses `next_run_at`, never re-enables a paused scheduler, records trigger `manual`); the in-flight map returns 409 while a run is live. Edits recompute `next_run_at` from now under the new schedule (edit at 09:30 from 09:00→10:00 daily fires today 10:00). Disable keeps everything including the derived label and history; enable resumes at the next future occurrence. Failed runs retry naturally at the next occurrence (no in-place backoff in v1 — a 15-min job retries in 15 min, a nightly one tomorrow; backoff/alerting is the deferred operational group).

**D10 — Permissions: `scheduler.read` / `scheduler.write`, backfill migration.** Read granted to all built-in roles (screens are visible), write to Owner/Admin (+ Superadmin's workspace set) — the `agents.*` pattern. Backfill follows the mandatory idempotent-migration rule (000031/000022 precedents): built-in roles snapshot permissions at creation and are never re-synced at runtime.

**D11 — API shape.** `GET/POST /workspaces/:ws/schedulers`, `GET/PATCH/DELETE /workspaces/:ws/schedulers/:id`, `POST …/run`, `GET …/runs` (paginated, newest-first). Handlers follow the existing workspace-scoped handler + guard pattern; store methods take explicit workspace scope (no query without it). The `schedule` tool and HTTP handlers share `domain.ValidateScheduler` — one validator, two surfaces; the tool's extra rule (channel target ⇒ agent is a member) lives at the tool layer.

**D12 — Data model.** `schedulers` table (migration 000044): id, workspace_id (CASCADE), agent_id (CASCADE), created_by (SET NULL), name, prompt, kind, expr (nullable, recurring only), run_at (nullable, once only), delivery JSONB, enabled, next_run_at (nullable), last_run JSONB, timestamps; `UNIQUE (workspace_id, agent_id, name)`; partial index on `next_run_at WHERE enabled AND next_run_at IS NOT NULL`. Validation lives in the domain layer (charset + real parse), not DB constraints.

## Risks / Trade-offs

- [Concurrent claim correctness] → Dedicated Postgres integration test: two simultaneous claim transactions, one due row, exactly one wins; plus a same-tick duplicate-claim test. This is the highest-risk statement in the change and gets the first test written.
- [Transcript read path may assume an `agent_sessions` row] → Verify-first task in group 3; fallback is a narrow read-path exemption keyed on the `sched_` prefix. Discovery cannot invalidate the design (worst case is one guarded branch).
- [Checkpoint skip may not be separable in the session adapter] → Time-boxed spike in group 3; default is keep-checkpoints. Either outcome is spec-compliant.
- [Unbounded run-session growth] → Documented v1 limit (design decision D7; ~100k sessions/yr worst case for a 5-min schedule; realistic schedules are 30–70 MB/yr). First destructive-path candidate for a future retention change; not silently deferred — recorded here and in the proposal.
- [Scheduler posts can summon agents via channel mention parsing] → Intentional (a digest that @mentions Beacon should summon it), but it means scheduler output can trigger token spend beyond the run. Mitigated by hooks (`pre_tool_use` gates apply to the summoned run too) and by channel membership validation at create time.
- [In-memory in-flight map loses state on restart] → Acceptable: after a crash the run is gone (drain writes `last_run` only on outcome; an interrupted run surfaces as the next claim's catch-up), and a fresh process has an empty map — worst case a manual run-now double-fires after a crash mid-run, the same exposure channels accept for their link registry.
- [`NO_REPLY` false positives] → Whole-word, case-insensitive, exact-final-reply matching only; the prompt convention lives in the unattended contract so only scheduler runs are taught the token.

## Migration Plan

1. Migration 000044 creates `schedulers` + the permission backfill; it is additive and rollback-safe (down migration drops the table and removes the granted permission strings).
2. Deploy order is unconstrained: the feature is dormant until schedulers exist; the ticker starts empty. No existing endpoint, event, or stored payload changes shape — the origin rename touches values nothing has ever emitted.
3. Rollback = disable the ticker (env) or revert; leftover scheduler rows are inert data.

## Open Questions

- Whether the runs listing reads a prefix-query over session events or a dedicated lightweight projection (per-scheduler run index) — settle in group 2/3 against real query plans; behavior identical.
- Exact unattended-contract wording (final copy pass before group 3; affects prompt snapshots in tests only).
