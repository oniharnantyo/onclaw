## Purpose

Per-user, per-agent memory (`agent_user_memories`): the user's own view and reset of what an agent remembers about them, while all memory writes stay runtime-owned — never exposed on the management API.

## ADDED Requirements

### Requirement: Own-memory view and reset
Any workspace member SHALL be able to view and reset **their own** memory with an agent via `GET/DELETE /workspaces/:ws/agents/:agent/memory`. Access SHALL require membership only (plus `agents.read` implicitly via route membership) — no separate permission — since the data is the user's own. Unknown agents and non-members get 404 indistinguishably. Reset SHALL be immediate and destructive (204) with no undo.

#### Scenario: Member reads own memory
- **WHEN** a member GETs their memory with agent "atlas"
- **THEN** 200 {content, created_at, updated_at} (empty content when the runtime has not written anything yet)

#### Scenario: Member resets own memory
- **WHEN** a member DELETEs their memory with an agent they have memory with
- **THEN** 204; a subsequent GET returns empty content

#### Scenario: Non-member gets 404
- **WHEN** a non-member requests, or the agent slug is unknown
- **THEN** response is 404, indistinguishable

### Requirement: Runtime-owned writes
Memory content SHALL only be written by the runtime (future change) service-side. The management API SHALL NOT accept memory content on any endpoint, and regenerating prompts SHALL NOT touch memories.

#### Scenario: No memory write endpoint
- **WHEN** any management-API request carries memory content
- **THEN** it is ignored; no API exists to write memory content
