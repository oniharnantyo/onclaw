# agent-identity

## Purpose

Assemble each agent's system prompt from a small set of markdown files: one global user-facts
file shared across all agents, plus per-agent persona/memory/capabilities files in each
agent's workspace, layered with the agent's `system_prompt`.

## ADDED Requirements

### Requirement: A global USER.md holds user facts shared across all agents

The system SHALL maintain a single global `USER.md` at the onclaw home root
(`~/.onclaw/USER.md`) containing user-level facts shared by every agent. It SHALL be assembled
first into every agent's system prompt. Missing or empty, it SHALL contribute nothing.

#### Scenario: The global user file is shared

- **WHEN** `~/.onclaw/USER.md` exists with the user's role and preferences
- **THEN** every agent's system prompt includes that content first

### Requirement: Per-agent persona/memory files live inside the agent's workspace

The system SHALL assemble each agent's system prompt from optional markdown files in that
agent's workspace: `IDENTITY.md`, `SOUL.md`, `CAPABILITIES.md`, `USER.md` (agent-specific user
facts), `AGENTS.md`, and `MEMORY.md`. Missing files SHALL be skipped without error, and empty
files SHALL be treated like missing files. These files are **per-agent**: each agent's workspace
holds its own set, not shared. The assembled prompt SHALL be capped to a maximum byte size.

#### Scenario: Present per-agent files are included

- **WHEN** the `coder` workspace contains `SOUL.md` and `CAPABILITIES.md`
- **THEN** the coder agent's system prompt includes their contents

#### Scenario: Missing per-agent files are skipped

- **WHEN** an agent's workspace has no per-agent files
- **THEN** the agent still starts, using the global `USER.md` (if any), its `system_prompt`, and the base instruction

### Requirement: The system prompt layers global, per-agent, and role content

The system SHALL assemble the final system prompt by concatenating, in order: the global
`USER.md`, the per-agent workspace files (in a fixed order), the agent's `system_prompt`
(cross-ref `agent-profiles`), then the workspace grounding.

#### Scenario: All layers are present

- **WHEN** the global `USER.md` and a per-agent `SOUL.md` exist and the agent row has a `system_prompt`
- **THEN** the prompt contains the global `USER.md`, then `SOUL.md`, then the `system_prompt`, then grounding

### Requirement: Each persona/memory file ships a template and is seeded from it

The system SHALL ship a default markdown template for each persona/memory file under
`internal/agent/templates/` (`identity.md`, `soul.md`, `capabilities.md`, `user.md`,
`memory.md`, `agents.md`), embedded into the binary. During `onclaw init` and `onclaw agent
add`, the system SHALL seed each file in the agent's workspace from its matching template,
and SHALL seed the global `~/.onclaw/USER.md` from `user.md` — only when the target file is
absent (non-destructive). A file's default content SHALL be defined by its template file, not
by a hardcoded constant.

#### Scenario: init seeds each file from its template

- **WHEN** `onclaw init` runs for the master agent and its workspace has no persona files
- **THEN** each file is created from its shipped template, and `~/.onclaw/USER.md` is created from `user.md`

#### Scenario: Existing files are preserved

- **WHEN** `onclaw init` runs and the user has already customized a file
- **THEN** that file is left unchanged
