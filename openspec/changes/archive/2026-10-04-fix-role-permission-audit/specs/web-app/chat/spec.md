## MODIFIED Requirements

### Requirement: Tool approval prompt
When an execution pauses on a dangerous shell command, the transcript SHALL render a pending-approval card in place of the tool-call result: the card SHALL show the command text with an approve and a deny action, and SHALL replace itself with the eventual tool-call result (output or denial notice) once the approval is resolved. The approve/deny actions SHALL be actionable for holders of `agents.write` and for the session's owning member; any other viewer sees the card read-only with a "waiting for the owner or an admin" hint. Approvals whose tool call would write through a service connection SHALL render actionable only for holders of `integrations.write`; other viewers — including the session's owner — see them read-only. The card SHALL render for approvals that are pending from a previous page load or server restart, not only for live interruptions. While the approval is pending, the turn SHALL be presented as paused rather than failed or completed.

#### Scenario: Live approval card
- **WHEN** an `approval_required` event arrives in the live transcript
- **THEN** a pending-approval card appears with the command text and approve/deny actions

#### Scenario: Resolution replaces the card
- **WHEN** the user approves or denies from the card
- **THEN** the card is replaced by the tool result (command output or denial notice) without a full page reload

#### Scenario: Reloaded pending approval
- **WHEN** the transcript is loaded while an approval is pending from an earlier execution
- **THEN** the pending-approval card renders from durable history and remains actionable

#### Scenario: Member resolves their own approval
- **WHEN** the session's owning Member activates approve or deny on a shell-command approval card in their own session
- **THEN** the actions are enabled and the resolution is submitted

#### Scenario: Member sees another user's approval read-only
- **WHEN** a Member without agents.write views a pending approval on a session they do not own
- **THEN** the card renders read-only with a hint naming the owner or admins

#### Scenario: Connection approval read-only for member
- **WHEN** a pending approval would write through a service connection and the viewer holds no integrations.write
- **THEN** the card renders read-only with a "waiting for an Owner or Admin" hint, even for the session's owner
