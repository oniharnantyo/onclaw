## ADDED Requirements

### Requirement: The agent handler chain includes a memory middleware

The agent assembly SHALL include a memory middleware in its handler chain alongside
summarization, history, skill, and hooks. The middleware SHALL inject the curated memory core at
session start and SHALL run memory extraction after agent runs. Memory loading SHALL replace the
prior silent truncation of `MEMORY.md` under the shared persona-file budget with an explicit,
self-contained cap.

#### Scenario: The memory middleware is part of the assembled agent

- **WHEN** an agent is assembled
- **THEN** the handler chain includes the memory middleware and `MEMORY.md` is loaded under the memory core's own cap
