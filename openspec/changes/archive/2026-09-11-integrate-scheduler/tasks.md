# Tasks: integrate-scheduler

## 1. Domain and store

- [x] 1.1 Rename origin: `OriginCron` → `OriginScheduler` with wire value `"scheduler"` in `internal/agents/events.go` (normalizeOrigin, comments), update hooks event docs/spec references in code
- [x] 1.2 Add `internal/domain/scheduler.go`: Scheduler entity (kind recurring|once, expr, run_at, prompt, delivery target struct, enabled, next/last run), `ValidateScheduler` (name, prompt, 5-field parse via robfig/cron/v3, once requires future instant, delivery target shape), `NextRun(expr, after, tz)`, human-label derivation
- [x] 1.3 Add `robfig/cron/v3` to go.mod
- [x] 1.4 Add `SchedulerStore` sub-interface to `internal/store/store.go`; implement the in-memory fake
- [x] 1.5 Migration 000044: `schedulers` table per design D12 (CASCADE/SET NULL FKs, unique (workspace_id, agent_id, name), partial due index) + idempotent `scheduler.read`/`scheduler.write` built-in-role backfill
- [x] 1.6 Postgres `SchedulerStore` implementation (workspace-scoped, pgx)
- [x] 1.7 Domain tests: validator table (valid/invalid exprs, once instants, delivery shapes, name uniqueness), NextRun across a DST boundary in a non-UTC tz, fake store behavior

## 2. Ticker and claim

- [x] 2.1 Claim SQL: `UPDATE … WHERE id IN (SELECT … FOR UPDATE SKIP LOCKED) RETURNING` writing future `next_run_at` at claim; once-scheduler grace → `missed` + archived
- [x] 2.2 Integration test: two simultaneous claim transactions on one due row — exactly one claims; overlapping-tick duplicate-claim test; overdue recurring row fires once and reschedules
- [x] 2.3 `internal/scheduler` Service: clock-injected loop (15s tick, `ONCLAW_SCHEDULER_TICK`), dispatch goroutines, global concurrency semaphore (~4), graceful stop (WaitGroup drain)
- [x] 2.4 Service tests (fake store, injected clock): due dispatch, pause skips, once archives after fire, missed-grace path, restart catch-up fires once

## 3. Runner integration

- [x] 3.1 Verify-first: transcript read path resolves a session by id without an `agent_sessions` row; add the narrow `sched_` read exemption if it joins through the index (design D7)
- [x] 3.2 Spike, time-boxed: events-without-checkpoints toggle in the session adapter; adopt if clean, else keep checkpoints and record the finding in design.md (D7)
- [x] 3.3 Scheduler execution profile in composition: origin branch composes AGENTS/IDENTITY/SOUL + workspace metadata (no shared memory) + unattended contract; omits USER.md, BOOTSTRAP.md, channel docs
- [x] 3.4 Tool-resolution exclusion for `OriginScheduler`: strip `schedule` and memory tools regardless of allowlist/workspace gate
- [x] 3.5 Runner tests: profile composition snapshot (documents present/absent, contract last), tool strip, interactive-run composition unchanged, per-run session id shape `sched_<schedulerID>_<ts>`

## 4. Drain and delivery

- [x] 4.1 Drain (mirrors channels fanout): tally tool calls, capture final text/turn/duration/tokens/status from the event tap; per-run timeout (`ONCLAW_SCHEDULER_RUN_TIMEOUT`, default 10m)
- [x] 4.2 `last_run` writeback (status completed|failed|cancelled|blocked|missed, duration, tokens, session id)
- [x] 4.3 Delivery: thread (no-op), channel via chokepoint Post as agent-author; `NO_REPLY` whole-word suppression records suppressed delivery; delivery-failure recorded without failing the run
- [x] 4.4 Preflight in fireJob: creator live-resolved (disabled/missing → auto-pause + `blocked`), agent config resolution failure → `blocked` with no model call
- [x] 4.5 Drain tests: NO_REPLY matching table, delivery-failure-not-run-failure, blocked preflight paths

## 5. API and wiring

- [x] 5.1 Add `scheduler.read`/`scheduler.write` to the domain permission catalog
- [x] 5.2 Handlers: list/create/get/update/delete, run-now (409 on in-flight, trigger `manual`, works while paused without re-enabling), runs listing (paginated newest-first)
- [x] 5.3 Router + composition root wiring: construct `scheduler.Service` after the runner with granular store sub-interfaces + `runner.Run`; start with server lifecycle; env knobs
- [x] 5.4 Handler tests: permission gates (member 403 on write), validation errors surface field-level, workspace isolation, 409 in-flight

## 6. Schedule tool

- [x] 6.1 Register the `schedule` tool: create/list/update/delete actions against `SchedulerStore` via `domain.ValidateScheduler`; tool card metadata (name/icon_key)
- [x] 6.2 Tool-layer rule: channel target requires agent membership; one-shot requires resolvable future instant; tool available only in non-scheduler runs (assert the strip from 3.4 covers it)
- [x] 6.3 Tool tests: create happy path, duplicate name rejection, non-member channel target, listing scoped to the acting workspace

## 7. Web

- [x] 7.1 API client: scheduler CRUD + run-now + runs endpoints; origin rename carried in any origin-typed unions
- [x] 7.2 Schedules screen on live data: table (name→editor, human label, next run, last-run→runs link, toggle, run-now), empty state, toast on delete
- [x] 7.3 Schedule editor modal: friendly recurrence builder (presets → generated expr, time picker, day chips), custom cron mode with validation + next-runs preview, task prompt textarea, delivery target selector (thread default / channel select), read-only workspace tz — per the approved mockup
- [x] 7.4 Runs screen on live data: scheduler runs table, trigger chips (`scheduler`/`manual`), filters, row → run transcript; transcript renders run sessions with the scheduler-origin marker (chip names the schedule)
- [x] 7.5 Web tests: builder generates expected expressions (preset matrix), editor validation states, runs table rendering; update suites touched by the cron→scheduler copy rename

## 8. Smoke and docs

- [x] 8.1 Smoke suite: create scheduler (API) → run-now → poll run record → transcript present; channel-delivery scheduler posts to the channel; member-role 403; pause/run-now-while-paused
- [x] 8.2 AGENTS.md domain vocabulary: Cron/Schedule entry → Scheduler (name, schedule, task, delivery target, next/last run)
- [x] 8.3 Manual pass: full dashboard flow (builder → enable → run-now → runs → transcript) and a chat-created scheduler via the tool — VERIFIED 2026-09-11 evening: builder form (structured fields, recurrence presets with human-readable preview, delivery, enabled) created "Evening status sweep" (0 9 * * 1-5) with toast; run-now executed (completed, 5s, 1.9k tokens); Runs screen renders the run row with trigger/status/duration/tokens and filters; the row opens the run's transcript in the agent chat with the scheduler-origin marker; the model's reply (SCHED-MANUAL-PASS) present in transcript and DB. Chat-created scheduler: initially the model had NO schedule tool — the composition root's runner registry (internal/cli/server.go) never passed `WithSchedulerTools` (it existed only on the hooks tool-value listing registry; task 6.1 gap) — fixed by passing `WithToolRegistry(NewDefaultToolRegistry(memories, WithSchedulerTools(schedulers, channels)))` in both cli/server.go and the router fallback runner; the same fix applied to server/router.go for parity. After the fix the model called the schedule tool directly and `morning-digest` (0 8 * * *, enabled) persisted via /v1. Two web fixes made during the pass: SchedulesView/RunsView alive-ref StrictMode hang (ref never reset after the dev double-invoke cleanup — spinner stuck forever) fixed by resetting `alive.current` in the effect body.
