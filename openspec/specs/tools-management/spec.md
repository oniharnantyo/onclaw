# tools-management Specification

## Purpose
TBD - created by archiving change add-tools-management. Update Purpose after archive.
## Requirements
### Requirement: Builtin tools declare a category

The system SHALL require every builtin tool to declare a category via its tool metadata, and
the management surface SHALL group tools by category. A tool SHALL belong to exactly one
category.

#### Scenario: Tools are grouped by category in the management API

- **WHEN** the client requests the tool list
- **THEN** tools are returned grouped by category

#### Scenario: Every builtin has a category

- **WHEN** a builtin tool is registered without a category
- **THEN** registration is rejected

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

### Requirement: Per-category configuration storage

The system SHALL store per-category configuration as JSON in a `tool_group_config` table
keyed by category, and SHALL treat this table as the runtime source of truth for
configurable categories. A category SHALL expose configuration only if it has registered a
configuration schema; categories without a registered schema SHALL expose no configuration
surface. When a category has no stored row, the system SHALL apply code defaults.

#### Scenario: A configurable category persists edited config

- **WHEN** configuration for a registered category is written
- **THEN** it is stored in `tool_group_config` and read back identically

#### Scenario: A non-configurable category exposes no config

- **WHEN** a category has not registered a configuration schema
- **THEN** the management surface offers no configuration control for it

### Requirement: Tools management REST API

The system SHALL provide auth-required REST endpoints: list tools grouped by category with
each category marked configurable or not, toggle a tool's global enable flag, and get/set a
category's configuration. Mutations SHALL take effect on subsequent agent runs without a
restart.

#### Scenario: Toggling a tool via the API persists

- **WHEN** a client POSTs a toggle for a tool
- **THEN** the `tool_registry` row is updated and the next tool list reflects the new state

#### Scenario: Setting category config via the API persists

- **WHEN** a client PUTs configuration for a configurable category
- **THEN** the `tool_group_config` row is updated

#### Scenario: Unknown category config is rejected

- **WHEN** a client requests config for a category with no registered schema
- **THEN** the request is rejected

### Requirement: Tools management Web UI

The system SHALL provide a Tools view in the Web UI that lists builtin tools grouped by
category, with a toggle per tool and a configuration control beside each configurable
category header. Controls SHALL persist changes and confirm them to the user. Non-builtin
(MCP) tools SHALL NOT appear in this view.

#### Scenario: A user toggles a tool in the UI

- **WHEN** the user toggles a tool off in the Tools view
- **THEN** the change persists and a confirmation is shown

#### Scenario: A configurable category shows a Config control

- **WHEN** a category is configurable
- **THEN** its header displays a Config control that opens an editor for that category's settings

### Requirement: Opt-in with no behavioral change

When every tool is globally enabled (the default) and no category configuration has been edited, the agent SHALL behave identically to a system without tools management.

#### Scenario: Default state is a no-op

- **WHEN** no tool has been toggled and no category config edited
- **THEN** agent behavior is unchanged from before tools management

