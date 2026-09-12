# Proposal: integrate-scheduler

## Why

OnClaw agents can only act when a human sends a message. Teams need standing orders — a morning digest posted to `#ops`, a nightly incident sweep, an hourly pipeline check — executed by an agent on a schedule without anyone at the keyboard. The web UI already ships a client-mock schedules screen, the event model already reserves a scheduler run origin, and the channels chokepoint already proves the submit-and-drain pattern; the missing piece is the backend that makes schedules real.

## What Changes

- Add a **scheduler** entity: a named, workspace-scoped job binding an agent, a task prompt, a recurrence (5-field cron in the workspace timezone), and a delivery target (`thread` default, or a channel).
- Add **one-shot schedulers** (`kind: once` with `run_at`) that auto-archive after firing, with an overdue grace window.
- Add a **scheduler service**: an in-process ticker that claims due jobs with `FOR UPDATE SKIP LOCKED` (multi-instance safe), fires each as an agent run with `Origin: scheduler`, and records run outcomes. Overdue recurring jobs fire once and reschedule — never replay.
- Each scheduler run executes in its **own fresh session** (`sched_<schedulerID>_<ts>`), persisted as transcript events but **not indexed in the per-user chat sidebar** — runs are artifacts listed per schedule on the Runs screen.
- Scheduler runs use a **trimmed execution profile**: `USER.md`, `BOOTSTRAP.md`, shared memory, and channel docs are omitted from composition; an unattended-run contract is appended; the `schedule` and memory tools are stripped from the run's toolset (a scheduled run cannot mint schedulers or edit memories).
- Scheduler runs execute as the **creating user, resolved at fire time**; a disabled or removed creator auto-pauses the scheduler (`blocked`), and invalid agent/provider config blocks before any token spend.
- Run delivery: the final reply stays in the run's session (`thread`), or is posted to a target **channel** through the existing chokepoint. A final reply of exactly `NO_REPLY` suppresses channel delivery.
- Add a **`schedule` agent tool** so agents can create, list, update, and delete schedulers from chat (the chat flow renders as a normal tool card).
- Add scheduler CRUD + run-now + runs-listing REST endpoints guarded by new `scheduler.read` / `scheduler.write` permissions, with the idempotent built-in-role backfill migration.
- Web: the schedules screen moves from client mock to live API with a **user-friendly recurrence builder** (presets, day/time chips — raw cron notation demoted to a "Custom" escape hatch), and the Runs screen lists scheduler runs. UI copy renames cron → scheduler throughout.
- Rename the reserved run-origin wire value `cron` → `scheduler` (zero live consumers today; hooks' origin values and matchers follow).

Deferred explicitly: origin-chat delivery (posting results back into the creating conversation — needs a new write path + mockup), external message-channel targets (e.g. Telegram), run retention (growth is documented, no deletion path in v1), failure alerting/auto-disable.

## Capabilities

### New Capabilities

- `scheduler`: The scheduling system — scheduler entity and validation, store, ticker and claim semantics, firing (preflight, acting identity, per-run sessions, trimmed profile hooks), delivery targets, one-shots, the `schedule` agent tool, REST API, and permissions.

### Modified Capabilities

- `agent-runtime`: New requirement — scheduler execution profile (reduced document composition, tool strip, unattended contract, per-run session handling).
- `agent-hooks`: Run-origin value `cron` renamed to `scheduler` in event payloads and matcher values.
- `web-app/schedules`: Editor replaced by a friendly recurrence builder with a task prompt field and delivery-target picker; create-from-chat becomes the `schedule` tool flow; terminology renames.
- `web-app/runs`: Runs screen scoped to scheduler runs with trigger values `scheduler` / `manual`; rows link to the schedule.
- `web-app/chat`: Cron-origin transcript marker renamed to scheduler-origin marker.

## Impact

- **New code**: `internal/domain/scheduler.go`, `internal/scheduler/` (service + drain), `internal/server/handlers/schedulers.go`, `SchedulerStore` (fake + postgres), migration 000044 (+ permission backfill).
- **Modified code**: `internal/agents/events.go` (origin rename + normalize), runner composition/tool-resolution branches for the scheduler profile, `internal/store/store.go` (new sub-interface), router + composition root wiring, web schedules/runs screens and API client.
- **New dependency**: `github.com/robfig/cron/v3` (5-field parsing + next-run computation) — the only new module.
- **Wire changes**: hook event `origin` values; new REST endpoints; no changes to existing endpoint contracts.
- **Risk points**: concurrent-claim correctness (integration test with two simultaneous claimers), transcript read path for non-indexed sessions, checkpoint-free session adapter feasibility.
