## ADDED Requirements

### Requirement: Memory tools auto-seed into the registry

The `memory_search`, `session_search`, and `memory` tools SHALL auto-seed into `tool_registry` as
enabled by default, following the existing tools-management seeding flow. The tools SHALL inherit
the existing redaction decorator so secrets are masked in their inputs and outputs.

#### Scenario: Memory tools appear in the seeded registry

- **WHEN** the tool registry is seeded
- **THEN** `memory_search`, `session_search`, and `memory` are present and enabled by default
