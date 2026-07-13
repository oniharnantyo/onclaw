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

The enable flag SHALL also govern builtin tools that are injected by the Eino filesystem middleware
(`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `execute`) even though those tools
are not assembled through the tool factory. A toggle middleware SHALL wrap each such tool's call and
withhold any tool whose global enable flag is false, so toggling takes effect on subsequent agent
runs without a restart — the same guarantee the spec makes for factory-assembled tools.

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

#### Scenario: A middleware-injected tool respects the global enable flag

- **WHEN** a filesystem-middleware tool (e.g. `glob`) has its global enable flag set to false and an
  agent run invokes it
- **THEN** the toggle middleware withholds the call and returns a disabled result, without a process
  restart

## ADDED Requirements

### Requirement: Middleware-injected tools are seeded into the registry

The system SHALL seed the builtin tools injected by the Eino filesystem middleware into
`tool_registry` on startup alongside factory-assembled tools, with a default of enabled, so they
appear in the management API and Web UI grouped by category and are toggleable there. The seeded
tools are `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep` (category `Filesystem`) and
`execute` (category `Shell`).

#### Scenario: Filesystem-middleware tools appear in the seeded registry

- **WHEN** the tool registry is seeded
- **THEN** `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, and `execute` are present,
  enabled by default, and grouped under their `Filesystem` / `Shell` categories

#### Scenario: Middleware tools are grouped by category in the management API

- **WHEN** the client requests the tool list
- **THEN** the filesystem-middleware tools are returned grouped under the `Filesystem` and `Shell`
  categories alongside the other builtin tools
