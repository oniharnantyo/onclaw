## ADDED Requirements

### Requirement: An agent stores its own memory configuration

The system SHALL persist a per-agent memory configuration on the agent row as a JSON document (`agents.memory_config`). The configuration SHALL round-trip through agent create and edit without loss, and SHALL default to an empty document for agents created before the column existed. The configuration SHALL be editable through the agent management UI and the agent API.

#### Scenario: A saved memory configuration is preserved across edits

- **WHEN** an agent's memory configuration is updated and the agent is later edited for an unrelated field
- **THEN** the previously saved memory configuration is unchanged

#### Scenario: A pre-existing agent gets an empty configuration

- **WHEN** the `memory_config` column is added to a database that already contains agents
- **THEN** every existing agent's `memory_config` is `{}` and the agent behaves as the global defaults

## MODIFIED Requirements

### Requirement: Named agents are stored in an `agents` table and selected per run

The system SHALL store agent definitions in an `agents` table (name, provider, model, model metadata, reasoning control, system prompt, workspace, tools, max iterations, memory configuration, enabled). `onclaw run` and `onclaw chat` SHALL select the agent to use from a `--agent <name>` flag, or when absent from a `default_agent` preference. The selected agent SHALL be the only agent that runs for that invocation. Running with no `--agent` and no `default_agent` SHALL fail with a clear error.

#### Scenario: The default agent is used when `--agent` is absent

- **WHEN** a `default_agent` preference is set to `coder` and the user runs `onclaw run "hi"`
- **THEN** the `coder` agent runs

#### Scenario: An agent's memory configuration is stored alongside its definition

- **WHEN** an agent row is read for assembly
- **THEN** the row includes the agent's `memory_config`, used to derive the effective memory configuration