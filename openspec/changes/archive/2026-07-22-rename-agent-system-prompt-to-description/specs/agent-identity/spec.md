## MODIFIED Requirements

### Requirement: Per-agent persona/memory files live inside the agent's workspace

The system SHALL assemble each agent's system prompt from optional markdown files in that agent's workspace: `IDENTITY.md`, `SOUL.md`, `CAPABILITIES.md`, `USER.md` (agent-specific user facts), `AGENTS.md`, and `MEMORY.md`. Missing files SHALL be skipped without error, and empty files SHALL be treated like missing files. These files are **per-agent**: each agent's workspace holds its own set, not shared. The assembled prompt SHALL be capped to a maximum byte size.

#### Scenario: Present per-agent files are included

- **WHEN** the `coder` workspace contains `SOUL.md` and `CAPABILITIES.md`
- **THEN** the coder agent's system prompt includes their contents

#### Scenario: Missing per-agent files are skipped

- **WHEN** an agent's workspace has no per-agent files
- **THEN** the agent still starts, using the global `USER.md` (if any) and the base instruction (workspace grounding)

### Requirement: The system prompt layers global, per-agent, and role content

The system SHALL assemble the final system prompt by concatenating, in order: the global `USER.md`, the per-agent workspace files (in a fixed order), then the workspace grounding. The system prompt SHALL NOT include the agent's `description`. The agent's `description` (cross-ref `agent-profiles`) SHALL instead be passed to the eino agent as its `Description` — agent-to-agent routing metadata — and SHALL NOT be sent to the model as part of the system prompt.

#### Scenario: All layers are present

- **WHEN** the global `USER.md` and a per-agent `SOUL.md` exist and the agent row has a `description`
- **THEN** the system prompt contains the global `USER.md`, then `SOUL.md`, then grounding, and does **not** contain the `description`

#### Scenario: The description is agent metadata, not a prompt layer

- **WHEN** an agent row has a non-empty `description`
- **THEN** the description is passed as the eino agent's `Description` and is absent from the model's system message