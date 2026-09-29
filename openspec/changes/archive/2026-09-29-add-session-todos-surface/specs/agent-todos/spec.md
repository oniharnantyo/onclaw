# Spec Delta

## ADDED Requirements

### Requirement: Session todos chip
The direct-chat view SHALL render a todos chip in the chat header whenever the session's current todo list — the item list of the newest `todo_write` call in the session's loaded events — contains at least one item, and SHALL render no chip otherwise. The chip SHALL display a spinner-state marker with the active item's text while any item is active, and a done/total progress count. The chip SHALL reflect the current list live as `todo_write` events arrive in the running session, without a page reload. Chip presence SHALL follow the existing present-only rule: no chip when the agent does not expose `todo_write` or the session has no plan.

#### Scenario: Chip shows current progress
- **WHEN** the session's newest `todo_write` holds 5 items with 2 done and 1 active
- **THEN** the chip renders with the active item's text in spinner state and a 2/5 progress count

#### Scenario: Chip absent without a plan
- **WHEN** the session's events contain no `todo_write` call, or the newest call's list is empty, or the agent does not expose `todo_write`
- **THEN** no chip renders anywhere in the chat header

#### Scenario: Chip updates live
- **WHEN** a `todo_write` event arrives during a running session marking a second item done
- **THEN** the chip's progress count and active-item marker update without a reload

### Requirement: Todos popover
Activating the chip SHALL open an anchored popover showing the session's current todo list in full: a header identifying the agent and the list revision, and one row per item with the same four row states the transcript cards use — done struck and dimmed, active spinning, pending dimmed, failed in the danger color with its failure reason beneath. The popover SHALL close on an explicit dismiss control and on interaction outside it.

#### Scenario: Popover shows the full current list
- **WHEN** the user activates the chip on a session whose current list has a failed item with a reason
- **THEN** the popover lists every current item with its status styling and shows the failed item's reason

#### Scenario: Popover dismisses
- **WHEN** the user activates the dismiss control or clicks outside the open popover
- **THEN** the popover closes and the chip remains

### Requirement: Todos auto-surface
The popover SHALL open itself when the first `todo_write` event of a run arrives, and SHALL collapse back to the chip when the run finishes or when the current list reaches a state with no open items. Later `todo_write` events in the same run SHALL update the open popover in place but SHALL NOT re-open a popover the user dismissed during that run.

#### Scenario: Auto-open on first plan
- **WHEN** an agent run emits its first `todo_write` call
- **THEN** the popover opens without user action

#### Scenario: Auto-collapse on completion
- **WHEN** the run finishes or every item in the current list is done
- **THEN** the popover collapses to the chip

#### Scenario: Dismissal sticks within a run
- **WHEN** the user dismisses the auto-opened popover and the same run later emits another `todo_write`
- **THEN** the popover stays closed and only the chip updates

### Requirement: Todos on small viewports
Below the responsive floor at which the anchored popover cannot fit, the popover SHALL render as a bottom sheet over the chat view with the same content and row states; the chip SHALL be unchanged. The surface SHALL introduce no horizontal overflow at any supported viewport.

#### Scenario: Bottom sheet at the responsive floor
- **WHEN** the chat view renders at 360×800 and the user activates the chip
- **THEN** the plan opens as a bottom sheet with the full current list, and the layout has no horizontal overflow

### Requirement: Transcript revision collapse
The transcript SHALL render a full todo card only for the final `todo_write` call of each turn; every earlier `todo_write` call — same-turn rewrites and earlier turns' calls alike — SHALL collapse to a one-line summary noting the plan was updated. The persistent surface (chip and popover) SHALL be the presentation of the current state; collapsed entries preserve the revision history without restating it.

#### Scenario: Only turn finals keep cards
- **WHEN** a transcript holds three turns whose runs emitted two, one, and three `todo_write` calls respectively
- **THEN** three full cards render (the final call of each turn) and the other three calls render as one-line plan-updated summaries
