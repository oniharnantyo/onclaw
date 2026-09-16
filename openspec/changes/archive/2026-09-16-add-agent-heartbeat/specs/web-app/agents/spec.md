## ADDED Requirements

### Requirement: Agent heartbeat configuration
The agent configuration modal SHALL offer a Heartbeat section with exactly one labeled control per property: an enable toggle, a cadence control — a friendly select covering intervals (every 5–12 hours and 5/15/30 minutes), daily-at, and weekly-on schedules — with a derived human label and next-run preview, plus an advanced collapsible (collapsed by default, auto-opened when populated) holding an optional active-hours pair of time pickers and a raw cron override with the 5-minute floor, a delivery selector (creator gateway DM by default, or an explicit workspace channel), the HEARTBEAT checklist as an editable monospace textarea with a reset-to-default action, a Run now action, and a status line showing the derived cadence label, next tick time, and last tick outcome. Raw JSON editing MUST NOT be offered for any heartbeat value. The section SHALL require `agents.write` to mutate and SHALL render read-only otherwise. When the heartbeat is auto-paused, the section SHALL show a paused banner with the failure reason and Resume and Run now actions. UI layout MUST be approved through an ASCII gallery before implementation.

#### Scenario: Enable with defaults
- **WHEN** a member with agents.write toggles the heartbeat on and saves
- **THEN** the heartbeat is created with the default checklist, a 30-minute cadence, no active hours, creator-DM delivery, and the status line shows the next tick time

#### Scenario: Cadence select generates the expression
- **WHEN** the user picks "Every 30 minutes" in the cadence select and saves
- **THEN** the stored expression is the equivalent 5-field cron and the status line shows its derived label

#### Scenario: Paused banner offers resume
- **WHEN** the heartbeat was auto-paused after consecutive failures
- **THEN** the section shows the paused banner with the last error, and Resume re-enables it with a recomputed next tick

#### Scenario: Run now reports the outcome
- **WHEN** the user triggers Run now
- **THEN** the section surfaces that a manual tick started and the last-tick status updates when it finishes
