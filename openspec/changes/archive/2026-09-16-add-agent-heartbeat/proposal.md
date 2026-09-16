# add-agent-heartbeat — Proposal

## Why

OnClaw agents are purely reactive today: a run starts only on a user turn, a channel message, a gateway message, or an explicit scheduler fire. Peer assistants (OpenClaw, Hermes) converge on a "heartbeat" — an ambient, opt-in periodic wakeup where the agent reviews its checklist and proactively reports only when something needs attention, staying silent otherwise. Both peers pin exactly one heartbeat per heartbeat-owner (OpenClaw: per agent; Hermes: per instance) and route the many-checks need to their cron equivalent — OnClaw's schedulers already fill that role, so a 1:1 agent heartbeat is the missing proactive layer, not a scheduler duplicate.

## What Changes

- New `agent_heartbeats` table: exactly one opt-in heartbeat per agent (UNIQUE per agent) with a 5-field cron cadence (workspace timezone), an optional active-hours window, a HEARTBEAT checklist prompt, a delivery target, and runtime tick state (`next_tick_at`, `last_tick`, `failure_streak`). Migration 000052 (+ `heartbeat_runs` per-tick records mirroring `scheduler_runs`).
- New `internal/heartbeat` ticker service mirroring the scheduler claim loop (SKIP LOCKED claim, env knobs `ONCLAW_HEARTBEAT_TICK` / `ONCLAW_HEARTBEAT_RUN_TIMEOUT` / `ONCLAW_HEARTBEAT_CONCURRENCY`).
- New run origin `heartbeat` (`agents.OriginHeartbeat`) joined to the hooks origin value set.
- Heartbeat execution profile in the runner: scheduler-style trimmed composition plus the agent's HEARTBEAT checklist and a workspace-activity digest; `schedule` and memory tools stripped.
- Persistent session model: every tick of an agent appends to one shared `hb_<agentID>` session; auto-compaction bounds token rent. Heartbeat ticks never count as user activity.
- Silence contract: a tick whose entire final reply is `NO_REPLY` (the existing scheduler D8 token) records `completed` + `delivery_status=suppressed` and produces zero outbox rows and zero channel posts.
- Cost/sanity guards: empty checklist skips the tick with no model call; ticks defer while the agent has a run in flight; ticks outside the active-hours window skip; auto-pause after 5 consecutive failed ticks with manual resume.
- Delivery: creator's paired gateway DM (via the gateways outbox) by default, optional explicit channel override (via the channel chokepoint); the run session transcript always keeps the full record.
- Heartbeat sub-resource API on agents (`GET/PUT .../heartbeat`, `POST .../heartbeat/run-now`, `POST .../heartbeat/resume`), riding existing agents permissions (no new permission catalog entries).
- Web: Heartbeat section in the agent configuration modal (enable, cadence presets + custom cron, active hours, delivery, checklist editor, run-now, paused banner).

## Capabilities

### New Capabilities
- `agent-heartbeat`: the per-agent heartbeat entity and API, the tick engine's claim/fire/drain loop, the persistent hb_ session model, silence suppression, delivery to gateway DM or channel, skip/defer/auto-pause guards, and per-tick run records.

### Modified Capabilities
- `agent-hooks`: the hook event catalog's origin enumeration gains `heartbeat` (heartbeat runs evaluate workspace+agent hooks like scheduler runs do).
- `agent-runtime`: gains a heartbeat execution profile requirement (trimmed composition + HEARTBEAT checklist + activity digest, stripped recursive/interactive tools) mirroring the existing scheduler execution profile.
- `web-app/agents`: the agent configuration modal gains a Heartbeat section (structured controls per property, no raw JSON).

## Impact

- **Backend:** new `internal/heartbeat` package; `internal/agents` (origin constant, normalize set, runner profile branch); `internal/domain` (Heartbeat entity + validation, reusing scheduler cron machinery); `internal/store` (+fake+postgres HeartbeatStore); `internal/server` (handlers, router wiring, gateways-runtime style service start); `internal/gateways` outbox consumed for DM delivery; `internal/channels` chokepoint reused for channel delivery.
- **Schema:** migration `000052_agent_heartbeat` (up/down); session binding validators accept the `hb_` prefix.
- **Frontend:** `web/src` agent modal Heartbeat section, `api.ts`/`gateways.ts`-style client additions.
- **Tests:** domain validation, fake + postgres store, ticker fire/skip/defer/pause paths, silence suppression end-to-end, runner profile, handler CRUD, web component tests, smoke section.
