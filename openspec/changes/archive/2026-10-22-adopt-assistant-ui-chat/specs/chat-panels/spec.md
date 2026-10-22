# Spec Delta

## MODIFIED Requirements

### Requirement: Run card tasks lane

The chat view SHALL show a floating run card over the transcript while the active session has background subagent tasks, listing each task's description and live status (running with a spinner, or its terminal completed/failed/stopped result), derived from the session's task events so the lanes rebuild correctly after a transcript reload. The card MUST render only while at least one lane has content.

The run card SHALL float at the top-right of the transcript's scrolling viewport, clear of the conversation map, and MUST NOT overlap the composer at any viewport height or scroll position. The card SHALL stay in place while the transcript scrolls beneath it.

#### Scenario: Running task surfaces in the run card

- **WHEN** the agent spawns a background task during a turn
- **THEN** the run card shows the task as running with its description until a terminal status arrives

#### Scenario: Lanes rebuild after reload

- **WHEN** the session is reloaded after a task completed earlier
- **THEN** the run card shows the task's terminal result derived from the replayed events

#### Scenario: Run card stays clear of the composer

- **WHEN** the run card is visible and the window is short enough that the transcript viewport is only a few lines tall
- **THEN** the card sits at the top-right of the transcript viewport and no part of it covers the composer or the conversation map

#### Scenario: Run card holds while the transcript scrolls

- **WHEN** the user scrolls the transcript while the run card is visible
- **THEN** the card remains anchored at the viewport's top-right as the entries move beneath it

### Requirement: Run card plan lane

The run card SHALL show the session's current plan as a checklist derived from the latest plan-list tool activity, with each item's pending/active/done/failed state, updating live during the turn. The plan lane SHALL render inside the same floating card as the tasks lane and shares its anchoring. When the session has no plan data, the plan lane MUST not render, and with no lane content the run card is hidden entirely.

#### Scenario: Plan updates live

- **WHEN** the agent rewrites its plan mid-turn
- **THEN** the run card checklist reflects the new items and their states

#### Scenario: No plan, no lane

- **WHEN** the session has not used the plan tool
- **THEN** the run card does not show a plan lane (and hides entirely if the tasks lane is also empty)
