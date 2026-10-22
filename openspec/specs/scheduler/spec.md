# scheduler Specification

## Purpose

Run an agent on a recurring schedule without anyone at the keyboard: a schedule binds an agent and a prompt to a cron recurrence, fires through the background gateway for as long as the process is running, produces an ordinary chat session per run, and is managed either from a dedicated Schedules menu or by an agent through its tool.

## Requirements

### Requirement: Schedule definition

A schedule SHALL consist of a name, a target agent, a prompt, a recurrence expressed as a five-field cron expression interpreted in the host's local time zone, and an enabled flag. Creating or updating a schedule SHALL reject an invalid cron expression, an unknown agent, an empty name, an empty prompt, or a name already used by another schedule (compared case-insensitively) with a readable error that names the offending field. Every enabled schedule SHALL expose its next fire time, computed from the recurrence and the current local time.

#### Scenario: Valid schedule is persisted with its next fire time

- **WHEN** a schedule is created with a valid name, an existing agent, a non-empty prompt, and the expression `0 9 * * 1-5`
- **THEN** it is stored enabled and reports the next weekday 09:00 local time as its next fire time

#### Scenario: Invalid cron expression is rejected

- **WHEN** a schedule is created with the expression `every monday`
- **THEN** the create is refused with an error naming the recurrence field and nothing is stored

#### Scenario: Unknown agent is rejected

- **WHEN** a schedule is created for an agent id that does not exist
- **THEN** the create is refused with an error naming the agent field

#### Scenario: Duplicate name is rejected

- **WHEN** a schedule named `Standup digest` exists and another is created named `standup digest`
- **THEN** the create is refused with an error naming the name field and identifying the existing schedule

### Requirement: Schedules fire while the process runs

While the gateway is Running, every enabled schedule SHALL fire within one minute of each of its recurrence times, regardless of whether the main window is visible and regardless of host (desktop or server). A disabled schedule SHALL NOT fire. Firing SHALL NOT require any user interface to be open.

#### Scenario: Fires with the window hidden

- **WHEN** an enabled schedule reaches its recurrence time while the desktop window has been closed to the tray and the gateway is Running
- **THEN** the schedule fires

#### Scenario: Disabled schedule does not fire

- **WHEN** a schedule is disabled and its recurrence time passes while the gateway is Running
- **THEN** no run is started and its last-run state is unchanged

#### Scenario: Server host fires identically

- **WHEN** the same schedule exists under the headless server host and its recurrence time passes
- **THEN** it fires exactly as it does on the desktop host

### Requirement: One session per run

Each fire SHALL create a new chat session for the schedule's agent, titled from the schedule name and the local fire time, and SHALL submit the schedule's prompt as that session's opening user message so the turn runs to completion unattended. The session SHALL appear in the ordinary chat surface with its complete transcript, including tool calls and approvals. Fires of the same schedule that overlap in time SHALL each get their own session; a still-running earlier run SHALL NOT block the next fire.

#### Scenario: Fire produces a visible titled session

- **WHEN** the schedule `Standup digest` fires at 09:00 on 2026-10-21
- **THEN** a session titled from `Standup digest` and `2026-10-21 09:00` exists for the schedule's agent, its first message is the schedule's prompt, and it is listed in the chat session list

#### Scenario: Overlapping fires are independent

- **WHEN** a schedule's previous run is still in progress at its next recurrence time
- **THEN** a second session is created and run without waiting for the first

### Requirement: Paused and missed fires are skipped

A fire that comes due while the gateway is Paused, or that came due while the process was not running, SHALL be skipped rather than executed late. The schedule SHALL record the most recent missed fire time, its next fire time SHALL advance to the next recurrence after the current time, and the next fire SHALL proceed normally once the gateway is Running. Missed fires SHALL be visible on the schedule in the Schedules menu.

#### Scenario: Fire due while paused is marked missed

- **WHEN** the gateway is Paused from the tray and a schedule's recurrence time passes
- **THEN** no run is started, the schedule shows that fire time as missed, and its next fire time is the following recurrence

#### Scenario: Fire due while the process was quit is marked missed on launch

- **WHEN** the application was fully quit across a schedule's recurrence time and is launched afterwards
- **THEN** that fire is not executed, the schedule shows it as missed, and the next fire time is the next recurrence after launch

#### Scenario: Resuming the gateway fires the next recurrence

- **WHEN** the gateway is Resumed and the schedule's next recurrence time passes
- **THEN** the schedule fires

### Requirement: Last-run state

Each schedule SHALL record its last run as a time and an outcome — `ran` (a session was created and the prompt submitted), `failed` (a run could not be started, with the reason), or `missed` — and, for `ran`, the session the run produced. The Schedules menu SHALL display each schedule's next fire time and its last run time with outcome, and the last run SHALL open its session when one exists.

#### Scenario: Completed fire is reflected on the row

- **WHEN** a schedule fires successfully
- **THEN** its row shows the fire time with outcome `ran`, and activating the last run opens the session created by that fire

#### Scenario: Run that could not start is reported

- **WHEN** a schedule fires but no session can be created (for example its agent's provider is misconfigured)
- **THEN** its row shows the fire time with outcome `failed` and the reason, and the next fire time still advances

### Requirement: Run now

A schedule SHALL be runnable on demand. Running now SHALL use the same run path and record last-run state the same way as a scheduled fire, SHALL work regardless of the schedule's enabled flag and the gateway's pause state, and SHALL NOT change the schedule's next fire time.

#### Scenario: Run now on a disabled schedule

- **WHEN** the user runs a disabled schedule now
- **THEN** a session is created and the prompt submitted, the row shows the run as `ran`, and the schedule remains disabled with its next fire time unchanged

### Requirement: Schedules menu

The navigation rail SHALL offer a Schedules destination alongside Chat and Agents, in both the expanded and collapsed rail states and on the mobile bar. The Schedules view SHALL list every schedule — whether created manually or by an agent — showing its name, agent, recurrence in human-readable form, enabled toggle, next fire time, and last run; SHALL offer add, edit, delete, and run-now actions; SHALL update without a reload when schedules change or fire; and SHALL render legibly in both themes. With no schedules, the view SHALL show an empty state that offers manual add and notes that an agent can create schedules from chat.

#### Scenario: Rail destination opens the list

- **WHEN** the user activates the Schedules rail item
- **THEN** the content island shows the schedule list and the window strip identifies the view as Schedules

#### Scenario: Agent-created schedule appears live

- **WHEN** an agent creates a schedule during a chat turn while the Schedules view is open
- **THEN** the new schedule appears in the list without a reload

#### Scenario: Toggle from the row

- **WHEN** the user switches a schedule's enabled toggle off on its row
- **THEN** the schedule is disabled, its next fire time is cleared, and the change persists across a restart

#### Scenario: Empty state guides both paths

- **WHEN** no schedules exist
- **THEN** the view shows an empty state with an add action and a note that asking an agent in chat also creates schedules

### Requirement: Schedule editor

Adding and editing a schedule SHALL use one form with name, agent, prompt, enabled, and a recurrence builder. The builder SHALL offer presets — Minutely (every N minutes), Hourly (every N hours at a given minute), Daily (at a time), Weekly (on chosen weekdays at a time), Monthly (on a day of month at a time) — with plain inputs, and an Advanced mode accepting a raw five-field cron expression. The form SHALL show a human-readable summary of the recurrence and its next three fire times in local time, updating live as inputs change, and SHALL state that times are local. Saving SHALL be blocked with inline, field-level errors while any input is invalid. Editing SHALL open the same form prefilled; when the stored expression matches a preset shape the builder SHALL show that preset, otherwise it SHALL open in Advanced mode with the expression.

#### Scenario: Daily preset previews correctly

- **WHEN** the user picks Daily at 09:00 on 2026-10-21 at 10:00 local time
- **THEN** the summary reads as a daily 09:00 recurrence and the preview lists 2026-10-22, 2026-10-23, and 2026-10-24 at 09:00

#### Scenario: Weekly preset with several days

- **WHEN** the user picks Weekly on Monday and Friday at 17:30
- **THEN** the summary names both days and the time, and the preview lists the next three Mondays and Fridays in order

#### Scenario: Invalid cron is an inline error

- **WHEN** the user enters `0 25 * * *` in Advanced mode
- **THEN** the recurrence field shows an inline error, the preview is empty, and Save is disabled

#### Scenario: Edit reconstructs the preset

- **WHEN** the user edits a schedule stored as `30 17 * * 1,5`
- **THEN** the form opens on the Weekly preset with Monday and Friday selected and 17:30 set

#### Scenario: Non-preset expression opens in Advanced mode

- **WHEN** the user edits a schedule stored as `*/15 9-17 * * 1-5`
- **THEN** the form opens in Advanced mode showing that expression with its summary and preview

### Requirement: Deletion

Deleting a schedule SHALL remove it and SHALL leave the sessions its runs created untouched. Deleting an agent SHALL delete that agent's schedules.

#### Scenario: Deleting a schedule keeps its sessions

- **WHEN** a schedule that has produced sessions is deleted
- **THEN** the schedule is gone and every session it produced is still listed and openable

#### Scenario: Deleting an agent removes its schedules

- **WHEN** an agent with two schedules is deleted
- **THEN** both schedules are gone from the Schedules view

### Requirement: Scheduled runs are distinguishable to hooks

Hook events raised during a scheduled run SHALL carry the origin `scheduler`; hook events raised by a turn a user starts SHALL carry the origin `user`. The origin SHALL be present in the event a command hook receives so a hook can treat unattended runs differently.

#### Scenario: Command hook sees the scheduler origin

- **WHEN** a schedule fires for an agent that has a command hook on run started
- **THEN** the JSON event delivered to the hook's subprocess carries origin `scheduler`

#### Scenario: User turn keeps the user origin

- **WHEN** the user sends a message in chat to the same agent
- **THEN** the hook receives origin `user`
