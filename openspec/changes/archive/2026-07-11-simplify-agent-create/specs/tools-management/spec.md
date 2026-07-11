## MODIFIED Requirements

### Requirement: Global tool enable/disable

The system SHALL persist a global enable flag per builtin tool in a `tool_registry` table, seeded
from the builtin registry on startup with a default of enabled. Tool assembly SHALL exclude any
tool whose global enable flag is disabled, then apply the agent's per-tool allowlist. An **empty**
per-agent allowlist SHALL be treated as "all globally-enabled builtin tools allowed" — i.e. no
per-agent restriction — so an agent that carries no allowlist (the shape produced by
`onclaw agent add` and by the web create form) is offered every globally-enabled builtin tool. A
**non-empty** allowlist SHALL restrict the agent to the intersection of globally-enabled tools and
the allowlisted tool names. Toggling a tool SHALL take effect on subsequent agent runs without a
process restart.

#### Scenario: A disabled tool is withheld from the agent

- **WHEN** a tool's global enable flag is false and an agent runs
- **THEN** that tool is not offered to the model

#### Scenario: An empty allowlist offers all globally-enabled tools

- **WHEN** an agent with an empty per-agent allowlist (for example, a newly created agent) runs
- **THEN** every globally-enabled builtin tool is offered to that agent, subject only to feature
  gates such as memory-feature availability

#### Scenario: Global enable intersects a non-empty per-agent allowlist

- **WHEN** a tool is globally enabled but absent from an agent's non-empty allowlist
- **THEN** the tool is not offered to that agent

#### Scenario: Global enable survives restart

- **WHEN** a tool is toggled off and the process restarts
- **THEN** the tool remains disabled
