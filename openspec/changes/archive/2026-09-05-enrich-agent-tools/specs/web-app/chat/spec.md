# web-app/chat Delta

## ADDED Requirements

### Requirement: Tool approval prompt
When an execution pauses on a dangerous shell command, the transcript SHALL render a pending-approval card in place of the tool-call result: the card SHALL show the command text with an approve and a deny action, and SHALL replace itself with the eventual tool-call result (output or denial notice) once the approval is resolved. The card SHALL render for approvals that are pending from a previous page load or server restart, not only for live interruptions. While the approval is pending, the turn SHALL be presented as paused rather than failed or completed.

#### Scenario: Live approval card
- **WHEN** an `approval_required` event arrives in the live transcript
- **THEN** a pending-approval card appears with the command text and approve/deny actions

#### Scenario: Resolution replaces the card
- **WHEN** the user approves or denies from the card
- **THEN** the card is replaced by the tool result (command output or denial notice) without a full page reload

#### Scenario: Reloaded pending approval
- **WHEN** the transcript is loaded while an approval is pending from an earlier execution
- **THEN** the pending-approval card renders from durable history and remains actionable
