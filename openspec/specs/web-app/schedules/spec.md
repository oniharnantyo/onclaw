# web-app/schedules Specification

## Purpose

The cron screen — the workspace's recurring agent runs as a table with pause/resume and run-now — and the schedule editor modal for creating and editing schedules.

## Requirements

### Requirement: Schedule table
The schedules screen SHALL list every scheduler with its name and assigned agent, the recurrence's human-readable label (with the raw expression visible for custom recurrences), next fire time (or `—` when paused), last-run outcome with timestamp and duration, an enable/pause toggle, and a run-now action. The scheduler's name SHALL open its editor; its last-run cell SHALL link to the scheduler's runs. Paused schedulers SHALL retain their schedule, task, and history.

#### Scenario: Pause and resume
- **WHEN** the user toggles an enabled scheduler off and back on
- **THEN** the next-run cell reads `—` while paused and the schedule, task, and last-run data are unchanged on resume

#### Scenario: Run now
- **WHEN** the user triggers run-now on a scheduler
- **THEN** a new run appears in that scheduler's run list with trigger `manual` and status `running`, transitioning to `completed` moments later

#### Scenario: Empty state
- **WHEN** the workspace has no schedulers
- **THEN** the table area shows an empty state inviting the user to create one

### Requirement: Schedule editor
The editor modal SHALL provide labeled fields for scheduler name, assigned agent, recurrence built from friendly controls — frequency presets (hourly, daily, weekly, monthly) rendered as time and day-of-week pickers, plus a custom mode exposing the raw cron expression with a next-runs preview — a task prompt textarea, a delivery target (keep-in-thread by default, or a channel selector), and an enabled switch, with the timezone shown read-only from workspace settings. Save SHALL be disabled until the name is longer than one character, the task prompt is non-empty, and — in custom mode — the expression parses, showing an inline error otherwise. Editing an existing scheduler SHALL offer deletion.

#### Scenario: Built from presets without cron notation
- **WHEN** the user picks the daily preset, sets 09:00, and toggles Mon–Fri day chips
- **THEN** the editor reflects a human-readable summary such as "09:00 · Mon–Fri" and creates the scheduler without the user typing a cron expression

#### Scenario: Custom mode validates the expression
- **WHEN** the user switches to custom mode and types `at nine` into the expression field
- **THEN** save is disabled and the field shows an expression error with a next-runs preview absent

#### Scenario: Task prompt required
- **WHEN** the user saves with an empty task prompt
- **THEN** save is disabled and the prompt field shows a required error

#### Scenario: Delete scheduler
- **WHEN** the user deletes an existing scheduler from the editor
- **THEN** it disappears from the table and a confirmation toast appears

### Requirement: Chat-created schedulers
When an agent creates or modifies a scheduler through the `schedule` tool in a conversation, the transcript SHALL render the invocation as a tool card whose summary names the schedule, its recurrence in human-readable form, and its delivery target; the reply MAY confirm the schedule in plain words and offer an immediate test run.

#### Scenario: Schedule tool card
- **WHEN** an agent creates scheduler "staging-check" (daily 09:00, delivering to #ops) from a chat
- **THEN** the transcript shows a `schedule` tool card summarizing the schedule, its recurrence, and the `#ops` target — not raw tool-argument JSON
