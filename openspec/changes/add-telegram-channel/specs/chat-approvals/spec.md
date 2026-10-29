# chat-approvals Specification (delta)

## ADDED Requirements

### Requirement: Remote decision surface

A pending approval SHALL be resolvable from an authorized external channel (per the channels capability): the channel renders the pending request with approve and deny actions that carry the session identity and the pending interrupt identity, and a decision taken there submits through the same bound decision path as the desktop UI. A decision from the channel resolves exactly once and obeys every conflict rule of the bound path: a stale or already-resolved interrupt MUST fail with a conflict rather than affecting any run. Only a sender authorized by the channel MAY act on the buttons; an unauthorized tap MUST NOT resolve anything. The transcript card SHALL record the decision the same way regardless of which surface submitted it.

#### Scenario: Approve from the chat resumes the turn

- **WHEN** an authorized user taps Approve on a pending approval in the channel
- **THEN** the command executes and the turn continues to normal completion, identical to a desktop approval

#### Scenario: Unauthorized tap cannot resolve

- **WHEN** a sender not authorized by the channel taps the approval buttons
- **THEN** no decision is recorded and the approval stays pending

#### Scenario: Stale tap conflicts

- **WHEN** the buttons are tapped after the approval was already resolved elsewhere
- **THEN** the tap reports a conflict and no run state changes
