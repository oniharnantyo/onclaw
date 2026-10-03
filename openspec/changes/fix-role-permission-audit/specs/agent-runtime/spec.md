## MODIFIED Requirements

### Requirement: Cancellation at a safe point
A running execution SHALL be cancellable by an explicit cancel request addressing the run (workspace, agent, session). Cancellation SHALL be permitted to holders of `agents.write` and to the session's owning member (the user the session was created by); any other member SHALL be refused with 403. Cancellation SHALL take effect at a safe point — the in-flight model call or tool call either completes and is recorded, or is aborted with its partial state marked — and SHALL record a cancel marker in the session history. Cancellation SHALL NOT leave dangling tool-call records that would break the next execution on the thread. A consumer disconnecting — an HTTP request returning, a stream client closing, or a stream never being consumed — SHALL NOT cancel the run.

#### Scenario: Cancel between tool calls
- **WHEN** a caller cancels while tool calls are executing
- **THEN** in-flight calls finish or abort cleanly, a cancel marker is recorded, and a subsequent execution on the thread starts from a consistent history

#### Scenario: Consumer disconnect does not cancel
- **WHEN** the client that started a turn closes its connection before the turn completes
- **THEN** the run continues to completion and its events are persisted to session history

#### Scenario: Explicit cancel endpoint
- **WHEN** an authorized member — the session's owner, or a holder of agents.write — cancels the active run of a session
- **THEN** the run stops at the next safe point with a cancel marker, and the cancel endpoint reports the run as cancelled

#### Scenario: Non-owner member cancel refused
- **WHEN** a Member without agents.write cancels the active run of another user's session
- **THEN** response is 403 and the run continues

### Requirement: Dangerous-command approval
The shell tool SHALL classify each command against a built-in dangerous-pattern list (destructive filesystem operations, piping network fetches into a shell, privilege escalation, disk/format utilities, host power/reboot, and similar). A matching command SHALL NOT execute immediately: the runtime SHALL pause the execution, persist an `approval_required` transcript event carrying the command and an interrupt identifier, and wait for a human decision. A pending approval SHALL remain resolvable across server restarts. An approve decision SHALL execute the command and record its output as the tool result; a deny decision SHALL record a denial notice as the tool result without executing. Resolution SHALL be permitted through an authenticated endpoint to holders of `agents.write` and to the session's owning member; approval resolutions that escalate to service-connection writes SHALL additionally require `integrations.write` of the resolver regardless of ownership.

#### Scenario: Dangerous command pauses for approval
- **WHEN** an allow-listed shell tool is asked to run a command matching the dangerous list
- **THEN** the command does not execute, an `approval_required` event is recorded in the transcript, and the turn pauses

#### Scenario: Approval executes the command
- **WHEN** the session's owning member (or an agents.write holder) approves a pending approval
- **THEN** the command executes and its output appears in the transcript as the tool result

#### Scenario: Denial records a refusal
- **WHEN** a member denies a pending approval
- **THEN** the command never runs and the transcript records a denial notice as the tool result

#### Scenario: Approval survives a restart
- **WHEN** the server restarts while an approval is pending
- **THEN** the approval can still be resolved afterwards and the execution resumes from its checkpoint

#### Scenario: Connection-tool approval needs integrations.write
- **WHEN** the pending approval's tool call would write through a service connection and the resolver holds agents.write but not integrations.write
- **THEN** the resolution is refused with 403 and the approval remains pending
