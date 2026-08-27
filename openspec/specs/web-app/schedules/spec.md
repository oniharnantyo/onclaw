# web-app/schedules Specification

## Purpose

The cron screen — the workspace's recurring agent runs as a table with pause/resume and run-now — and the schedule editor modal for creating and editing schedules.

## Requirements

### Requirement: Schedule table
The cron screen SHALL list every schedule with its name and assigned agent, cron expression plus human-readable label, next fire time (or `—` when paused), last-run outcome with timestamp and duration, an enable/pause toggle, and a run-now action. Paused schedules SHALL retain their expression and history.

#### Scenario: Pause and resume
- **WHEN** the user toggles an enabled schedule off and back on
- **THEN** the next-run cell reads `—` while paused and the expression and last-run data are unchanged on resume

#### Scenario: Run now
- **WHEN** the user triggers run-now on a schedule
- **THEN** a new run appears in run history with trigger `manual` and status `running`, transitioning to `success` moments later

#### Scenario: Empty state
- **WHEN** the workspace has no schedules
- **THEN** the table area shows an empty state inviting the user to create one

### Requirement: Schedule editor
The editor modal SHALL provide labeled fields for schedule name, assigned agent, timezone (read-only, from workspace settings), cron expression, and human label, plus an enabled switch. Save SHALL be disabled until the name is longer than one character and the expression contains only digits, spaces, `*`, `/`, `,`, and `-`, showing an inline error otherwise. Editing an existing schedule SHALL offer deletion.

#### Scenario: Invalid expression
- **WHEN** the user types `at nine` into the expression field
- **THEN** save is disabled and the field shows "Digits, spaces, * / , - only."

#### Scenario: Create from chat
- **WHEN** the user sends `/schedule …` to an agent in a chat
- **THEN** the schedule editor opens prefilled with that agent and a default weekday-morning expression

#### Scenario: Delete schedule
- **WHEN** the user deletes an existing schedule from the editor
- **THEN** it disappears from the table and a confirmation toast appears
