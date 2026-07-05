## ADDED Requirements

### Requirement: The kg_search tool auto-seeds into the registry

The `kg_search` tool SHALL auto-seed into `tool_registry` as enabled by default, following the
existing tools-management seeding flow, and SHALL inherit the redaction decorator so secrets are
masked in its inputs and outputs.

#### Scenario: kg_search appears in the seeded registry

- **WHEN** the tool registry is seeded
- **THEN** `kg_search` is present and enabled by default alongside the slice-#1 memory tools
