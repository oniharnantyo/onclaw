## MODIFIED Requirements

### Requirement: Session todos chip
The direct-chat view SHALL present the session's todo plan inside the floating run card's Progress section — the card is the session's run-status surface, auto-surfaced over the transcript, hosting the todo checklist and the session's background-task activity (the Agents section, specified in web-app/chat). The todos data lane is unchanged: the section shows the item list of the newest `todo_write` call in the session's loaded events, with the same four row states the transcript cards use, an active-item spinner marker, and a done/total progress count in the card header. The section SHALL reflect the current list live as `todo_write` events arrive in the running session, without a page reload. Presence SHALL follow the existing present-only rule: the Progress section renders only when the agent exposes `todo_write` and the session has a plan; the card itself renders when either the todos lane or the background-task lane has content.

#### Scenario: Chip shows current progress
- **WHEN** the session's newest `todo_write` holds 5 items with 2 done and 1 active
- **THEN** the floating card's Progress section renders with the active item in spinner state and a 2/5 progress count in the header

#### Scenario: Chip absent without a plan
- **WHEN** the session's events contain no `todo_write` call, or the newest call's list is empty, or the agent does not expose `todo_write`
- **THEN** the Progress section does not render, while the card may still render for background-task activity alone

#### Scenario: Chip updates live
- **WHEN** a `todo_write` event arrives during a running session marking a second item done
- **THEN** the Progress section's rows and count update in place

## REMOVED Requirements

### Requirement: Todos popover

*Reason: the anchored header popover is superseded by the floating run card (agent-todos "Session todos chip" as modified, and web-app/chat "Floating run card") — the card is always the expanded surface and dismisses in place; there is no separate popover artifact.*
