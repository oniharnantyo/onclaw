## 1. Domain and Migration

- [x] 1.1 Add `domain.Heartbeat` entity + `ValidateHeartbeat` (trimmed checklist, 5-field cron ≥5-minute floor, active-hours `HH:MM` pair rejected when equal, delivery shape `creator_dm|channel`, derived `next_tick_at` never accepted from input) and `HeartbeatRun` mirroring `SchedulerRun`; reuse `NextRun`/`HumanLabel`; domain unit tests
- [x] 1.2 Create `migrations/000052_agent_heartbeat.up.sql` / `.down.sql` per design D1 (`agent_heartbeats` with UNIQUE(workspace_id, agent_id), partial due index; `heartbeat_runs` with CHECK enums and heartbeat+started index)
- [x] 1.3 Register the `hb_` session-id prefix with the agent-session binding validators (store refuses unregistered prefixes) and add the chan_-leak-style regression test twin for `hb_`
- [x] 1.4 Add embedded default HEARTBEAT checklist template (promptdocs pattern) with the silence contract wording

## 2. Store

- [x] 2.1 Add `store.HeartbeatStore` port (get/put/enable/pause/resume per agent, `ClaimDueHeartbeats` SKIP LOCKED with claim-time `next_tick_at` advance, start/finish run, streak update) + fake implementation and fake tests
- [x] 2.2 Implement the postgres adapter: CRUD mapping, due-claim query riding the partial index, run writes mirroring `scheduler_runs`, integration tests against the 000052 schema

## 3. Origin and Runner Profile

- [x] 3.1 Add `agents.OriginHeartbeat` to the origin constant set, `normalizeOrigin`, and `hooks.OriginValues()`; extend the hook origin-values test
- [x] 3.2 Add the heartbeat execution profile branch in the runner: scheduler-trimmed compose + HEARTBEAT checklist + activity digest injection; strip `schedule` + memory tools for origin heartbeat only; runner tests covering composition, tool strip, and other-origins-unchanged
- [x] 3.3 Implement the workspace-activity digest composer (channel message previews ≤120 chars, scheduler run outcomes + errors since `last_tick_at`, ~2KB cap, "no recent activity" empty state) with unit tests

## 4. Ticker Service

- [x] 4.1 Create `internal/heartbeat.Service` mirroring the scheduler service: ticker loop, claim dispatch, semaphore, detached fire context with `ONCLAW_HEARTBEAT_RUN_TIMEOUT` + drain grace, env knobs (`ONCLAW_HEARTBEAT_TICK` 30s, `ONCLAW_HEARTBEAT_CONCURRENCY` 2), Start/Stop
- [x] 4.2 Implement the fire path: preflight (creator resolved at fire time; disabled/deleted creator → blocked + auto-pause; missing agent → blocked), run-row lifecycle, drain to EOF mapping terminal events to statuses
- [x] 4.3 Implement the guards: empty-checklist skip (no model call), busy defer via the one-run-per-session/in-flight guard, active-hours skip; all three record `skipped` run rows, advance `next_tick_at`, and never touch the failure streak
- [x] 4.4 Implement the silence contract (`NO_REPLY` whole-reply case-insensitive → completed + `delivery_status=suppressed`, zero outbox/channel writes) and failure-streak accounting (increment on failed, reset on completed, auto-pause at 5, blocked ≠ failed); service tests for each path
- [x] 4.5 Implement delivery: creator-DM resolution to all paired chats across enabled gateways via the gateways outbox; channel override via the chokepoint; delivery failure never fails the tick; delivery tests with fakes
- [x] 4.6 Implement manual RunNow (works while disabled, never re-enables, never advances `next_tick_at`, 409 conflict in-flight) and Resume (re-enable + recompute + streak reset); tests

## 5. HTTP API

- [x] 5.1 Add handlers: `GET/PUT /workspaces/:slug/agents/:id/heartbeat` (agents.read/agents.write), `POST .../heartbeat/run-now`, `POST .../heartbeat/resume` (agents.write); conflict and validation error mapping per the house envelope
- [x] 5.2 Wire the heartbeat service into the server runtime next to the scheduler service (composition root injection, Start/Stop with the server lifecycle); router + handlers tests

## 6. Web

- [x] 6.1 Draw the Heartbeat-section ASCII gallery (all states: off, on, editing, paused banner, run-now in flight) and get user approval BEFORE building the UI (ui-first gate)
- [x] 6.2 Add the API client methods to `web/src/lib` (heartbeat get/put/run-now/resume payloads + types) with client tests
- [x] 6.3 Build the Heartbeat section in the agent configuration modal per the approved gallery: enable toggle, cadence presets + custom cron with derived label and next-run preview, active-hours pickers, delivery selector, checklist textarea with reset-to-default, Run now, status line, read-only rendering without agents.write; component tests

## 7. End-to-End Verification

- [x] 7.1 Add a smoke.sh section: create/enable heartbeat via API, run-now tick with a stubbed silent reply (verify suppressed + zero outbox rows), a report reply (verify outbox entry), active-hours skip, and auto-pause after forced failures
- [x] 7.2 Full verification pass: `go build ./...`, `go vet ./...`, `go test ./...`, `-tags=integration` suite, web vitest touched suites + tsc; fix loop until green
- [ ] 7.3 Manual pass: enable a heartbeat on a live agent with a real paired gateway, observe a silent tick and a report tick end-to-end, confirm the paused banner after induced failures, and confirm the hb_ transcript hydration in the sessions drill-down
