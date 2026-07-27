## MODIFIED Requirements

### Requirement: Global tool enable/disable

The system SHALL persist a global enable flag per builtin tool in a `tool_registry` table, seeded
from the builtin registry on startup with a default of enabled. Tool assembly SHALL exclude any
tool whose global enable flag is disabled, then exclude any tool named in the agent's per-agent
**denylist** (`agents.disabled_tools`). An **empty** denylist SHALL be treated as "all
globally-enabled builtin tools allowed" — i.e. no per-agent restriction — so an agent that carries
no denylist (the shape produced by `onclaw agent add`, the web create form, and the first-run
`master` seed) is offered every globally-enabled builtin tool, including tools added in future
versions without any manual enabling. A **non-empty** denylist SHALL withhold exactly the named
tools from that agent. The effective tool set for an agent SHALL be
`(globally-enabled builtin tools) MINUS (agent.disabled_tools)`. Toggling a tool SHALL take effect
on subsequent agent runs without a process restart.

The enable flag SHALL also govern builtin tools that are injected by the Eino filesystem middleware
(`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `execute`) even though those tools
are not assembled through the tool factory. A toggle middleware SHALL wrap each such tool's call and
withhold any tool whose global enable flag is false, so toggling takes effect on subsequent agent
runs without a restart — the same guarantee the spec makes for factory-assembled tools.

#### Scenario: A disabled tool is withheld from the agent

- **WHEN** a tool's global enable flag is false and an agent runs
- **THEN** that tool is not offered to the model

#### Scenario: An empty denylist offers all globally-enabled tools

- **WHEN** an agent with an empty per-agent denylist (for example, a newly created agent) runs
- **THEN** every globally-enabled builtin tool is offered to that agent, subject only to feature
  gates such as memory-feature availability

#### Scenario: A tool in the per-agent denylist is withheld

- **WHEN** a tool is globally enabled but present in an agent's non-empty denylist
- **THEN** the tool is not offered to that agent

#### Scenario: A tool absent from the denylist is offered without manual enabling

- **WHEN** a tool is globally enabled and absent from an agent's denylist, including a tool added in
  a new version that did not exist when the agent's denylist was curated
- **THEN** the tool is offered to that agent with no manual enabling or denylist edit

#### Scenario: Global enable survives restart

- **WHEN** a tool is toggled off and the process restarts
- **THEN** the tool remains disabled

#### Scenario: A middleware-injected tool respects the global enable flag

- **WHEN** a filesystem-middleware tool (e.g. `glob`) has its global enable flag set to false and an
  agent run invokes it
- **THEN** the toggle middleware withholds the call and returns a disabled result, without a process
  restart
