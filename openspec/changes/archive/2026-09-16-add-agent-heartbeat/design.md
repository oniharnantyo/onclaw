# add-agent-heartbeat — Design

## Context

OnClaw already runs unattended agent work through `internal/scheduler`: a SKIP LOCKED claim loop fires due schedulers into fresh isolated sessions, drains the event tap, and delivers channel-targeted results with a `NO_REPLY` suppression contract (scheduler D8). Heartbeats need the same bones with different flesh: continuity instead of isolation, ambient silence instead of a deliverable, and a human's pocket instead of a channel as the default target. Peer ground truth (2026-09-14 fetch): OpenClaw runs its heartbeat in the agent's main session with `isolatedSession` as the documented cost escape (~100K → ~2–5K tokens/tick), skips empty checklists without a model call, and defers ticks while the agent is busy; Hermes' community heartbeat plugin reuses the live gateway session precisely to keep continuity and re-reads a `HEARTBEAT.md` file each cycle. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- One opt-in proactive wakeup per agent with continuity across ticks, silence as the expected outcome, and delivery to a human's gateway DM.
- Reuse the scheduler's proven machinery wherever the semantics match: cron math, claim loop, preflight, drain, run records, tracing.
- Cost containment: skip-without-model-call, busy deferral, active hours, failure auto-pause, auto-compact on the persistent session.

**Non-Goals:**
- Multiple heartbeats per agent (schedulers own N-checks; endgame option: heartbeat as a scheduler delivery type — door left open, not built).
- Event-driven wakes ("wake me when X happens") — ticker cadence only in v1.
- Per-heartbeat model overrides, light-context flags, custom timezones (workspace timezone governs).
- Heartbeat sessions in the member private session index (ambient agent state, not a human conversation).

## Decisions

**D1 — One heartbeat per agent, own table.** `agent_heartbeats` with `UNIQUE (workspace_id, agent_id)`, mirroring the schedulers table conventions (uuid PKs, jsonb delivery, partial due-index, CHECK enums). Not columns on `agents`: `next_tick_at`/`failure_streak`/`last_tick` churn every tick and must not touch the agent row's `updated_at`/`updated_by` semantics. Alternative (jsonb config blob on `agents`) rejected for the same churn reason and for claim-query ergonomics.

**D2 — Persistent shared session per agent.** Every tick appends to one `hb_<agentID>` session (`hb_` prefix registered with the session binding validators — the chan_-leak class of bug is the failure mode if forgotten). The standard summarization middleware runs on the session, bounding token rent (OpenClaw's documented overflow-bleed hazard). Alternatives: fresh session per tick (scheduler-shaped, ~20–50× cheaper per OpenClaw's numbers, but loses continuity — the checklist would need a rebuilt digest every tick to know what it already checked); persistent-without-compact rejected as unbounded rent. Heartbeat ticks never update any "last activity" the private session index or retention logic reads. `heartbeat_runs.turn_id` best-effort links each tick row to its turn in the shared transcript.

**D3 — Checklist on the heartbeat row, seeded, re-read every tick.** `agent_heartbeats.prompt` holds the HEARTBEAT checklist (the locked promptdoc UX: edited in the agent modal like IDENTITY/SOUL are in the prompts tab). Create seeds an embedded default template (promptdocs-style embedded template, BOOTSTRAP precedent). An empty (or whitespace) prompt skips the tick with **no model call**, recording a `skipped` run row (`empty-heartbeat` reason) — OpenClaw steal.

**D4 — Cadence is the scheduler's cron machinery.** 5-field `expr` validated by `domain.ValidateScheduler`-grade parsing, `domain.NextRun` in the workspace timezone, `HumanLabel` for display; the web cadence UI generates the expr from a friendly select (intervals every 5/15/30 minutes and 1/2/4/6/12 hours, daily-at, weekly-on — the 2026-09-15 user-feedback rework of the original preset chips + raw cron input, which survives only as an advanced-collapsible override). No `once` kind — a one-shot ambient check is a scheduler job.

**D5 — Active hours: nullable `HH:MM` pair in the workspace timezone.** Both NULL = 24/7 (default). Equal start/end is a save-time 422 (OpenClaw documents zero-width = "always skipped" as a footgun; we reject instead of silently never firing). Outside the window the claim skips and records a `skipped` run row without token spend.

**D6 — Creator identity resolved at fire time (scheduler D5 preflight verbatim).** Deleted/disabled creator → tick blocked AND heartbeat auto-paused (creator grief); missing agent → blocked without pausing (repairable). Preflight precedes any model call.

**D7 — Silence is the existing `NO_REPLY` contract.** Whole-reply, case-insensitive match (scheduler D8 implementation reused). Silent tick: status `completed`, `delivery_status=suppressed`, zero outbox rows, zero channel posts. OpenClaw's legacy `HEARTBEAT_OK` (start/end-only, ≤300-char remainder) is deliberately not copied.

**D8 — Delivery: creator's paired gateway DMs by default; channel override.** `delivery = {"type":"creator_dm"}` resolves the creator's paired links across enabled gateways and enqueues one outbox entry per paired chat (at-least-once, existing outbox machinery); no pairing → transcript-only, not an error. `{"type":"channel","channel_id"}` posts through the channel chokepoint exactly like scheduler channel delivery. Delivery failure never fails the run (scheduler D8 split).

**D9 — Digest composition.** The heartbeat profile = scheduler-trimmed compose (AGENTS/IDENTITY/SOUL + workspace doc, no USER/BOOTSTRAP/channel docs) + the heartbeat checklist + a deterministic Go-composed activity digest since `last_tick_at`: channel message previews (author + 120 chars), scheduler run outcomes with errors, capped ~2KB; "no recent activity" when empty. Tools stay available (minus D10 strips) for deeper checks. Rationale: with a persistent session the model already remembers prior ticks; the digest only closes the gap for events that don't enter the session (channel chatter, scheduler outcomes).

**D10 — Tool surface strips.** Same anti-runaway strip as scheduler runs: `schedule` and memory tools removed regardless of allowlist (a heartbeat that reschedules itself or rewrites memory unattended is the Hermes anti-pattern). Everything else follows the agent allowlist and workspace gate.

**D11 — Origin `heartbeat` joins the hooks set.** `agents.OriginHeartbeat` + normalizeOrigin + `hooks.OriginValues()`. Hooks evaluate normally; a `user_prompt_submit` block ends the tick as `blocked` — blocked ≠ failed, does NOT increment `failure_streak` (policy grief, mirroring how scheduler treats hook blocks).

**D12 — Guards and auto-pause.** Busy defer: a due tick whose agent has a run in flight is skipped (`skipped`, reason `busy`) without queueing. `failure_streak` increments only on `failed` ticks; at 5 consecutive failures the heartbeat auto-pauses (`enabled=false`, `next_tick_at=NULL`) and the web shows a paused banner with Resume + Run now. Streak resets on any completed tick.

**D13 — API rides agent permissions; no new permission catalog entries.** `GET/PUT /workspaces/:slug/agents/:id/heartbeat` (agents.read / agents.write), `POST .../heartbeat/run-now` and `POST .../heartbeat/resume` (agents.write). Scheduler got its own permissions because it owns a screen; heartbeat has no own screen. If API-key scoping later demands a split, the backfill block (000044 pattern) lands then.

**D14 — Ticker service mirrors the scheduler service.** `internal/heartbeat.Service`: `ClaimDueHeartbeats` (SKIP LOCKED, claim-time `next_tick_at` advance — catch-up fires once, reschedules, never replays), semaphore-gated fires, detached fire context bounded by `ONCLAW_HEARTBEAT_RUN_TIMEOUT` (default 10m) + drain grace, env knobs `ONCLAW_HEARTBEAT_TICK` (default 30s) and `ONCLAW_HEARTBEAT_CONCURRENCY` (default 2 — ambient work stays light). Started/stopped from the server runtime next to the scheduler service.

**D15 — Run records + tracing mirror scheduler_runs**, including `trace_id` (000050 precedent) for Langfuse deep links; statuses `running|completed|failed|cancelled|blocked|skipped` with `delivery_status` carrying `delivered|suppressed|failed`.

## Risks / Trade-offs

- [Persistent-session token rent] → summarization middleware bounds context; interval floor enforced in domain validation (minimum 5m cadence); per-tick tokens visible in `heartbeat_runs` and the runs surface.
- [Silent-runaway: a misbehaving checklist that always reports] → silence is the default contract in the seeded template; failure/monitoring surfaces show delivery rates; hooks can gate `run_started` for origin `heartbeat`.
- [Digest privacy: channel previews enter ambient context] → workspace-internal data only, same exposure class as channel documents in normal runs; previews capped at 120 chars.
- [Multi-gateway duplicate DMs when a creator pairs several gateways] → accepted v1 (one entry per paired chat); the runs row records each delivery via outbox status.
- [`hb_` binding validator forget-me] → the chan_-leak regression test pattern gets a `hb_` twin in the same change.

## Migration Plan

`000052_agent_heartbeat.up/down` (create/drop the two tables). Feature ships dark: heartbeats are opt-in (`enabled=false` default, no rows exist until first enable), so deploy order is unconstrained and rollback is the down migration; any `hb_` session events written before rollback remain as inert transcript rows.

## Open Questions

None — the four load-bearing decisions (persistent session, HEARTBEAT promptdoc, creator-DM delivery, creator-at-fire-time identity) are user-locked; 1:1 cardinality adopted 2026-09-14.
