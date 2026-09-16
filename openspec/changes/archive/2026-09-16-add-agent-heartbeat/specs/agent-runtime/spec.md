## ADDED Requirements

### Requirement: Heartbeat execution profile
A run with origin `heartbeat` SHALL execute under the scheduler's trimmed profile — composed instruction containing `AGENTS.md`, `IDENTITY.md`, and `SOUL.md`, the workspace metadata document without the shared-memory subsection, and `USER.md`, `BOOTSTRAP.md`, and channel documents omitted — extended with two heartbeat-only sections: the agent's HEARTBEAT checklist prompt, and a workspace-activity digest composed since the heartbeat's previous tick (channel message previews, scheduler run outcomes with errors, capped in size, stating "no recent activity" when empty). The run's tool surface SHALL exclude the `schedule` tool and the memory tools regardless of the agent's allowlist or workspace gate; all remaining tools follow the normal allowlist and workspace gate.

#### Scenario: Heartbeat composition
- **WHEN** a heartbeat tick composes its instruction for an agent with all six documents and workspace shared memory present
- **THEN** the instruction contains AGENTS, IDENTITY, SOUL, the workspace metadata document, the HEARTBEAT checklist, and the activity digest, and does not contain `USER.md` content, `BOOTSTRAP.md` content, the shared-memory subsection, or channel documents

#### Scenario: Heartbeat tools stripped
- **WHEN** a heartbeat tick resolves its tool surface for an agent whose allowlist includes the `schedule` tool and memory tools
- **THEN** the run exposes neither — the remaining tools follow the normal allowlist and workspace gate

#### Scenario: Other origins are unchanged
- **WHEN** a user-initiated, channel-initiated, or scheduler-origin run executes for the same agent
- **THEN** composition and tool surface follow the existing requirements exactly — the heartbeat profile applies only to origin `heartbeat`
