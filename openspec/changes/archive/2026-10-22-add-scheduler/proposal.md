# Proposal

## Why

The buddies direction is a background gateway that keeps running in the tray with the window closed, "accepting triggers from cron, webhooks, Telegram chat, and more" — but no trigger exists yet: `internal/gateway` is a lifecycle seam with no adapters, the `gateway_settings` row (including its `default_agent_id`) is persisted but unused, and the hooks event `Origin` field reserves the string `"scheduler"` while every turn is still hardcoded as `"user"`. Cron-scheduled agent runs are the first trigger: they let an agent do recurring work (digests, checks, sweeps) unattended, and the gateway's start/pause/resume lifecycle already provides the switch they should obey.

## What Changes

- **Schedules.** A schedule binds a name, an agent, a prompt, and a recurrence (stored as a canonical 5-field cron expression, interpreted in local time) plus an enabled flag. Migration `000013` adds the `schedules` table with the agent FK and lightweight last-run state (last run time, status, resulting session id) and a persisted `next_fire_at`.
- **Cron trigger adapter on the gateway.** A minute-aligned ticker attached to the gateway manager fires due schedules — but only when the gateway is Running. The scheduler runs with the process, not the window: closing the window to tray never stops it. While the gateway is Paused (deliberate, from the tray) or the process was fully quit at a fire time, the fire is **skipped and marked missed**; the persisted `next_fire_at` makes the missed case detectable on next launch, and the row shows "missed <time>" while the next scheduled fire proceeds normally.
- **One chat session per run.** Each fire creates a fresh session for the schedule's agent and sends the prompt headlessly (the same `agents.Manager` path the composer uses, no UI required). Sessions are titled from the schedule and timestamp, and the transcript is fully visible in the normal chat surface. Runs record `Origin: "scheduler"` on hook events, replacing the hardcoded `"user"` with a plumbed-through origin.
- **A `scheduler` agent tool.** A new built-in catalog tool — browser-style, a single `action` argument (`create`, `list`, `update`, `delete`) taking only scalars — lets the user ask an agent in chat to create and manage schedules. Gated exactly like every other catalog tool: catalog membership, global Tools-pane enable, agent allowlist. It appears in the Tools pane card list and the agent allowlist chips without bespoke wiring.
- **A dedicated Schedules menu.** A new top-level navigation-rail destination (fourth sibling of Chat, Agents, Settings) renders the schedule list: enabled toggle, agent, recurrence in human-readable form, next run, and last-run status with a link to the run's session. Deleting an agent deletes its schedules.
- **A user-friendly editor.** One add/edit form with a preset builder (Minutely / Hourly / Daily / Weekly / Monthly segments with plain fields), an Advanced tab for raw cron, a live human-readable summary, and a "next 3 runs" preview in local time — so correctness is visible before saving. A **Run now** action fires a schedule immediately through the same path, for confidence.
- **Live updates.** The service emits a schedules-changed event on CRUD and after each fire so the menu reflects reality without polling.

Out of scope: webhook and Telegram adapters (later trigger types on the same seam), per-schedule timezones (local only), a full run-log table or history tab, natural-language recurrence parsing in the form, and system notifications on run completion.

## Capabilities

### New Capabilities

- `scheduler`: the schedule data model, the gateway cron adapter and its firing/missed/pause semantics, per-run headless sessions, the ScheduleService API surface, and the Schedules menu with its editor.

### Modified Capabilities

- `agent-tools`: the built-in tool catalog gains the `scheduler` tool, gated like every other catalog tool and listed in the Tools pane and allowlist chips without bespoke wiring.

## Impact

- **Go — gateway** (`internal/gateway/`): new cron adapter attaching to the existing `Manager` (Start/Stop lifecycle, honors Pause/Resume and State); composition wires it in `internal/cli/app.go` so both desktop and server hosts run it identically.
- **Go — store** (`internal/store/`, `internal/store/sqlite/`, `internal/store/fake/`): new `schedules` port + SQLite implementation + fake; migration `000013` (schema-version pin tests move to 13); `CASCADE` delete on the agent FK.
- **Go — agent runtime** (`internal/agents/`): run origin plumbed from the send path into hook events (today hardcoded `"user"` in the hooks adapter); no runtime behavior change for user turns.
- **Go — tools** (`internal/agents/builtin_tools.go`): `scheduler` catalog entry + constructor reaching the schedule store through the established config-source seam pattern; scalar-only input like `browser`.
- **Go — services** (`internal/services/`): new `ScheduleService` (CRUD, toggle, run-now) bound in `main.go` alongside the existing services; new typed event `schedules:changed` registered with the others.
- **Dependencies**: `github.com/robfig/cron/v3` for expression parsing and next-fire computation (new Go dependency).
- **Frontend** (`frontend/src/features/scheduler/`, `stores/nav.ts`, `App.tsx`, `lib/client.ts`, regenerated Wails bindings): new rail destination, schedule list + editor modal built from the shared ui kit (Segmented, Input, Select, Textarea, Toggle, ModalShell), next-runs preview, both themes.
- **Tests**: store CRUD + migration pin, adapter firing/missed/pause tests with a fake clock, tool action tests, service tests, and editor/list component tests including the cron-tab validation path.
