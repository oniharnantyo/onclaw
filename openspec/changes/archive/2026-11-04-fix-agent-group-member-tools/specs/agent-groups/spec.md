# Spec Delta

## MODIFIED Requirements

### Requirement: In-channel tools for group turns

A member's group turn SHALL be offered the member's ordinary tool surface — the same tool catalog through the same gate as an ordinary session turn, the workspace-rooted filesystem tools mounted on the member's own workspace, the knowledge corpus per its gate, and the agent's enabled capabilities — together with the group tools: reading the group transcript, assigning a child task to a named member, and reporting a task result. The group turn's input SHALL remain the bounded packet rather than a replay of the member's session log, and the group transcript reader SHALL be the only reader of the shared transcript. A reported result SHALL appear in the transcript as a task result and complete the task. An assignment SHALL create a child task owned by the chosen member, chained to the assigning task's root.

#### Scenario: Lead delegates through a tool

- **WHEN** the lead's group turn assigns a child task to another member
- **THEN** a task owned by that member is created, and its completion lands in the transcript as the member's result

#### Scenario: Group turn cannot read session history

- **WHEN** a member's group turn would read its own session log instead of the packet
- **THEN** no tool replays the session log into the turn, and the group transcript reader is the only reader of the shared transcript

#### Scenario: Group tools ride beside the work tools

- **WHEN** a member's group turn runs with the agent's ordinary tool gate unchanged
- **THEN** the turn's tool surface carries the ordinary catalog surface and the three group tools together

#### Scenario: Member turn keeps its workspace

- **WHEN** a member's group turn reads or writes a file
- **THEN** the filesystem tools answer rooted at the member's own workspace, and the knowledge corpus answers per its gate

#### Scenario: Agent allowlist still governs the work tools

- **WHEN** an agent's allowlist excludes a filesystem tool
- **THEN** the group turn does not offer it either, while the group tools remain available
