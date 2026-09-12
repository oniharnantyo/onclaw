## ADDED Requirements

### Requirement: Scheduler execution profile
A run with origin `scheduler` SHALL execute under a trimmed profile. The composed instruction SHALL contain `AGENTS.md`, `IDENTITY.md`, and `SOUL.md`, a workspace document carrying workspace metadata without the shared-memory subsection, and a closing unattended-run contract stating that the run executes unattended on a schedule, that the final reply is the deliverable, and that nothing worth reporting SHALL be stated plainly (the `NO_REPLY` convention). The profile SHALL omit `USER.md`, `BOOTSTRAP.md`, and channel documents entirely. The run's tool surface SHALL additionally exclude the `schedule` tool and the memory tools regardless of the agent's allowlist or workspace gate. Channel runs' context documents are not applicable and SHALL NOT be composed.

#### Scenario: Trimmed composition
- **WHEN** a scheduler run composes its instruction for an agent with all six documents and workspace shared memory present
- **THEN** the instruction contains AGENTS, IDENTITY, SOUL, and the workspace metadata document, does not contain `USER.md` content, `BOOTSTRAP.md` content, the shared-memory subsection, or channel documents, and ends with the unattended-run contract

#### Scenario: Tools stripped for unattended runs
- **WHEN** a scheduler run resolves its tool surface for an agent whose allowlist includes the `schedule` tool and memory tools
- **THEN** the run exposes neither — the remaining tools follow the normal allowlist and workspace gate

#### Scenario: Interactive runs are unchanged
- **WHEN** a user-initiated or channel-initiated run executes for the same agent
- **THEN** composition and tool surface follow the existing requirements exactly — the scheduler profile applies only to origin `scheduler`
