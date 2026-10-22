# chat-approvals Specification

## Purpose

Pause a turn before a dangerous shell command executes, record the approval request durably, let the single user approve or deny it from the transcript, and resume or abort the same turn based on the decision — including across an app restart.

## Requirements

### Requirement: Dangerous-command gate

A shell tool command matching the runtime's safety patterns (forced recursive deletion, pipe-to-shell downloads, privilege escalation, partition or disk writes, shutdown/reboot, fork bombs, and equivalent destructive forms) MUST NOT execute without an approval decision. On first encounter the tool call interrupts the run instead of executing, and the command runs only after approval. A recorded decision applies to exactly one occurrence: an identical command seen again after its decision was consumed MUST interrupt again and require a fresh decision.

#### Scenario: Dangerous command pauses the run

- **WHEN** the agent issues a command matching a safety pattern for the first time
- **THEN** the command is not executed and the turn pauses with a pending approval request

#### Scenario: Decision is consumed once

- **WHEN** an identical command runs again after a previous approval was applied
- **THEN** it interrupts again and requires a new decision

### Requirement: Durable approval requests

While a decision is pending, the interrupt payload and its execution checkpoint SHALL be persisted in the database. The turn has no terminal outcome while pending: the session stays busy, and further sends SHALL queue as they do during any in-flight turn instead of conflicting with the paused run. Pending state MUST survive an app restart — reopening the session shows the pending approval, and no history is lost.

#### Scenario: Pending approval survives restart

- **WHEN** the app quits while a command awaits approval and is later reopened
- **THEN** the session's transcript shows the same approval still pending, with the turn neither completed nor failed

#### Scenario: Sends queue behind a pending approval

- **WHEN** the user sends a message while an approval is pending
- **THEN** the message queues and is delivered only after the approval resolves and the resumed turn finishes

### Requirement: Decision side-channel

The decision SHALL be submitted through a bound service call carrying the session and the pending interrupt identity. Approving resumes the interrupted turn from its checkpoint so the command executes and the turn continues to its normal completion; denying feeds a denial result back to the tool in place of execution and the turn continues with the model informed. A decision for an interrupt that is no longer pending (stale id, already resolved) MUST fail with a conflict error rather than affecting any run. Cancelling the turn while an approval is pending SHALL discard the run and clear the pending request.

#### Scenario: Approve resumes the turn

- **WHEN** the user approves the pending command after an app restart
- **THEN** the command executes, and the turn continues streaming to a normal completion on the same session

#### Scenario: Deny informs the model

- **WHEN** the user denies the pending command
- **THEN** the tool result reports the denial to the model, the command never runs, and the turn continues

#### Scenario: Stale decision rejected

- **WHEN** a decision arrives for an interrupt that was already resolved or belongs to another session
- **THEN** the call fails with a conflict error and no run state changes

### Requirement: Approval card in transcript

A pending approval SHALL render as a card in the transcript showing the requested command and Approve/Deny actions, with the actions available only on the pending card. The card persists after the decision with its approved or denied outcome and, once the resumed turn produces it, the command's result. Replay of the transcript MUST reconstruct pending cards and resolved cards from the stored events.

#### Scenario: Card actionable while pending

- **WHEN** the transcript shows a pending approval card
- **THEN** it displays the command text with Approve and Deny actions enabled

#### Scenario: Resolved card on replay

- **WHEN** the transcript is reloaded after a decision was made and the turn finished
- **THEN** the card renders with its approved-or-denied outcome and the tool result below it
