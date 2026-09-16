## Purpose

Gives each workspace agent an opt-in proactive heartbeat: a periodic ambient wakeup in which the agent reviews its checklist plus workspace activity and either reports something needing attention or stays silent — with continuity across ticks in one persistent session.

## ADDED Requirements

### Requirement: Heartbeat entity
Each agent SHALL have at most one heartbeat, created disabled and addressed as a sub-resource of the agent. A heartbeat SHALL carry: the HEARTBEAT checklist prompt (seeded with a default template on creation; an empty checklist is valid and means skip-until-edited), a recurring 5-field cron expression, an optional active-hours window, a delivery target (creator gateway DM by default, or an explicit channel), and runtime state (`next_tick_at`, `last_tick` snapshot, `failure_streak`). The heartbeat's creator SHALL be recorded and resolved at fire time; agent deletion SHALL cascade-delete its heartbeat.

#### Scenario: First enable creates one heartbeat
- **WHEN** a workspace member with agents.write enables the heartbeat on an agent that has none
- **THEN** exactly one heartbeat row exists for that agent, disabled-by-default is overridden only by the explicit enable, `next_tick_at` is computed from the expression in the workspace timezone, and the checklist contains the seeded default template

#### Scenario: Second heartbeat per agent is rejected
- **WHEN** a create or enable request would leave an agent with a second heartbeat
- **THEN** the store rejects it on the per-agent uniqueness constraint and the API surfaces a conflict error

### Requirement: Cadence and active hours
The cadence SHALL be a valid standard 5-field cron expression evaluated in the workspace timezone; the human-readable label and next tick time SHALL always be derived, never stored from input. Active hours SHALL be a start/end `HH:MM` pair or absent; absent means 24/7, and an equal start/end pair MUST be rejected at save with a field error. The cadence MUST NOT be faster than once per 5 minutes.

#### Scenario: Active hours silence the night
- **WHEN** a heartbeat with an 08:00–22:00 active-hours window is claimed at 03:00 workspace time
- **THEN** the tick is recorded as skipped with no model call and `next_tick_at` advances to the next in-window occurrence

#### Scenario: Zero-width window rejected
- **WHEN** a heartbeat is saved with active hours 09:00–09:00
- **THEN** the save fails with a validation error naming the active-hours fields

### Requirement: Heartbeat API and permissions
The heartbeat SHALL be managed through agent sub-resource endpoints: read with `agents.read`, mutate (create/update/enable/pause) with `agents.write`, and run-now/resume with `agents.write`. No new permission catalog entries SHALL be introduced. Mutations that change the expression or active hours SHALL recompute `next_tick_at`; pausing clears it; resuming recomputes it.

#### Scenario: Member without write cannot enable
- **WHEN** a member holding only agents.read puts an updated heartbeat payload
- **THEN** the request is rejected with a permissions error and the heartbeat is unchanged

### Requirement: Due heartbeats fire claimed exactly once
A ticker SHALL claim due enabled heartbeats with a claim that advances `next_tick_at` atomically at claim time, so concurrent workers can never fire the same occurrence twice and a missed catch-up fires once and reschedules without replaying the skipped occurrences.

#### Scenario: Two ticks do not double-fire
- **WHEN** two ticker passes race on the same due heartbeat
- **THEN** exactly one claims it, advances `next_tick_at`, and fires one tick; the other pass sees nothing due

#### Scenario: Downtime catch-up fires once
- **WHEN** the server was down across three scheduled occurrences and restarts
- **THEN** the heartbeat fires once for the current claim and `next_tick_at` advances past all missed occurrences

### Requirement: Persistent tick session
All ticks of a heartbeat SHALL execute in one persistent shared session keyed `hb_<agentID>`, registered with the session binding validators so its transcript persists as session events. The session SHALL run the standard context summarization so its token rent stays bounded. Heartbeat turns MUST NOT update any last-activity marker the private session index or retention logic reads, and the session MUST NOT appear in any member's private chat index.

#### Scenario: Ticks accumulate in one transcript
- **WHEN** a heartbeat fires three ticks over three intervals
- **THEN** all three turns resolve to the same session id and the transcript contains all three in order

#### Scenario: Session index stays human-only
- **WHEN** the hb_ session persists events
- **THEN** no member's private session listing includes it

### Requirement: Preflight before token spend
Before any model call, the fire path SHALL resolve the heartbeat's creator: a deleted or disabled creator SHALL block the tick and auto-pause the heartbeat; a missing bound agent SHALL block the tick without pausing; infrastructure failures SHALL record the tick as failed. Preflight outcomes SHALL be visible as run rows.

#### Scenario: Disabled creator pauses the heartbeat
- **WHEN** a heartbeat's creator account is disabled and the heartbeat fires
- **THEN** the tick records as blocked with the reason, the heartbeat becomes disabled with `next_tick_at` cleared, and no tokens are spent

### Requirement: Silence contract
A tick whose entire final reply equals `NO_REPLY`, compared case-insensitively after trimming, SHALL complete with `delivery_status` of `suppressed`: no gateway outbox entry, no channel post, and the reply retained only in the tick transcript. Any other non-empty reply SHALL be treated as a report and delivered. The tick status SHALL be `completed` in both cases.

#### Scenario: Nothing to report stays silent
- **WHEN** a tick's final reply is exactly "no_reply" (any casing)
- **THEN** the tick completes as suppressed and no delivery surface receives a message

#### Scenario: A report is not silence
- **WHEN** a tick's final reply is "No reply needed, but note: disk 90% full"
- **THEN** the reply is delivered as a report, not suppressed

### Requirement: Delivery of non-silent output
Creator-DM delivery SHALL enqueue one gateway outbox entry per chat the creator has paired on every enabled gateway, using the existing at-least-once outbox; a creator with no paired chats yields transcript-only delivery and no error. Channel delivery SHALL post through the channel chokepoint exactly as scheduler channel delivery does. Delivery failure SHALL never fail the run — the tick stays completed with `delivery_status` of `failed` and the error recorded.

#### Scenario: Creator paired on two gateways gets both
- **WHEN** a report is delivered and the creator has paired links on Telegram and WhatsApp
- **THEN** one outbox entry is enqueued per paired chat and both eventually deliver

#### Scenario: Channel delivery failure does not fail the tick
- **WHEN** the chokepoint post for a channel-targeted report errors
- **THEN** the tick remains completed with delivery_status failed and the error stored on the run row

### Requirement: Skip and defer guards
The fire path SHALL skip a due tick without any model call when the checklist is empty (recorded with an empty-heartbeat reason), when the current instant is outside the active-hours window, or when the bound agent already has a run in flight (recorded as busy); skipped ticks SHALL advance `next_tick_at` normally and SHALL NOT count toward the failure streak.

#### Scenario: Empty checklist burns no tokens
- **WHEN** a heartbeat with a whitespace-only checklist is claimed
- **THEN** the tick is recorded as skipped with the empty-heartbeat reason and no provider call occurs

#### Scenario: Busy agent defers to the next tick
- **WHEN** the bound agent is mid-run when the heartbeat comes due
- **THEN** the tick is recorded as skipped with the busy reason and the model is not invoked

### Requirement: Failure streak auto-pause
`failure_streak` SHALL increment on each tick that ends failed and reset on any completed tick. After 5 consecutive failed ticks the heartbeat SHALL auto-pause (disabled, `next_tick_at` cleared) and only an explicit resume or run-now from an agents.write holder SHALL re-enable it. Hook-blocked ticks count as blocked, not failed, and MUST NOT increment the streak.

#### Scenario: Five failures pause the heartbeat
- **WHEN** five consecutive ticks end failed
- **THEN** the heartbeat is disabled with the streak retained, and the next due pass claims nothing

#### Scenario: Resume restarts the cadence
- **WHEN** an agents.write holder resumes a paused heartbeat
- **THEN** the heartbeat is enabled with `next_tick_at` recomputed and the failure streak reset

### Requirement: Manual run-now
Agents.write holders SHALL be able to fire a heartbeat immediately regardless of the enabled flag, through the same fire path with trigger `manual`; run-now MUST NOT re-enable a paused heartbeat, MUST NOT advance `next_tick_at`, and SHALL conflict while a tick for that heartbeat is already in flight.

#### Scenario: Run-now on a paused heartbeat
- **WHEN** run-now is called on a disabled heartbeat
- **THEN** one manual tick fires and records with trigger manual, and the heartbeat remains disabled

#### Scenario: Run-now conflicts with an in-flight tick
- **WHEN** run-now is called while a tick is executing
- **THEN** the request is rejected with a conflict error

### Requirement: Per-tick run records
Every tick SHALL record a run row with trigger (`tick` or `manual`), status, started time, duration, tokens used, delivery status, error, and the persisted trace id when the turn sampled in for export. Records SHALL list newest-first per heartbeat and link to the shared session transcript.

#### Scenario: Run record carries the trace link
- **WHEN** a tick completes and its turn exported a trace
- **THEN** the run row stores the trace id so the runs surface can deep-link to it
