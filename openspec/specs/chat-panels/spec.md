# chat-panels Specification

## Purpose

Provide side surfaces over the conversation: a tabbed right panel that previews workspace files, prompt documents, and subagent trajectory files, plus a floating run card summarizing plan progress and background tasks.

## Requirements

### Requirement: Right panel shell

The chat view SHALL provide a right panel presenting one preview at a time as tabs: it docks as a column at wide widths and renders as a full-height overlay sheet at narrow widths, remembers its width across sessions, and opens or closes without interrupting an in-flight turn. Each tab SHALL carry its own close affordance, and switching chat sessions SHALL reset the panel to closed.

#### Scenario: Open panel during a turn

- **WHEN** the user opens the right panel while the agent is responding
- **THEN** the panel appears beside the transcript and the turn continues uninterrupted

#### Scenario: Panel state resets on session switch

- **WHEN** the user switches to a different session
- **THEN** the panel is closed, ready to be reopened for the new session's content

### Requirement: Preview sources

The panel SHALL render previews for: files inside an agent's workspace opened by path (read through a workspace-scoped reader that MUST NOT reach outside the agent's directories), the agent's three prompt documents, and subagent trajectory files recorded for the session. Tool cards and transcript links to files or paths SHALL open the corresponding preview in the panel. Preview reads MUST be restricted to workspace and trajectory locations regardless of what the link contains.

#### Scenario: File path opens preview

- **WHEN** the user selects a file path offered by a completed tool card
- **THEN** the panel opens a tab showing that file's content from the agent's workspace

#### Scenario: Workspace reader stays jailed

- **WHEN** a preview request names a path outside any agent workspace (for example a system file)
- **THEN** the read is refused and no file content is returned

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
