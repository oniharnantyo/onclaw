## ADDED Requirements

### Requirement: Persona files are editable from the agent UI

The system SHALL expose the per-agent persona and memory markdown files in an agent's workspace (`IDENTITY.md`, `SOUL.md`, `CAPABILITIES.md`, `USER.md`, `AGENTS.md`, `MEMORY.md`) for reading and writing through the agent management UI and a guarded HTTP API. A read or write SHALL accept only a file name from the fixed persona set, SHALL strip any directory component, and SHALL resolve the path strictly within the agent's configured workspace. A write SHALL be security-scanned and rejected on a threat match. Edits SHALL take effect in the agent's next assembled session.

#### Scenario: A persona file is updated from the UI

- **WHEN** the user edits `SOUL.md` for the `coder` agent and saves
- **THEN** the file in the `coder` workspace is updated and the new content is present in the `coder` agent's next session prompt

#### Scenario: A path-traversal file name is rejected

- **WHEN** a request targets a file name outside the persona set or contains path separators
- **THEN** the request is rejected and no file outside the workspace is read or written