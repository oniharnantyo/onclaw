# Spec Delta — agent-runtime

## MODIFIED Requirements

### Requirement: Tool selection
An agent's tool surface SHALL be resolved from the tool catalog minus the agent's `disabled_tools` denylist, intersected with the workspace's enabled tool set (see the workspace-tools capability — the workspace gate wins over the denylist and over per-turn allowed-tools overrides). A name absent from `disabled_tools` SHALL expose the corresponding registered built-in tool. The reserved name `execute` SHALL be absent from the surface only when listed in `disabled_tools` (see "Shell execution"). The facade alias `browser` in `disabled_tools` SHALL disable the full browser tool set at resolution (see "Browser automation"); individual `browser.*` names in `disabled_tools` SHALL disable those tools individually. The filesystem middleware tools SHALL be governable through the denylist names `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`: a name present in `disabled_tools` SHALL disable that middleware tool via the filesystem middleware's per-tool disable configuration, a name absent SHALL keep it attached. An agent whose `disabled_tools` array is empty SHALL have every catalog tool exposed — registry tools and filesystem tools — subject to the workspace gate. Names in `disabled_tools` that match no registered tool SHALL be ignored, not errors. Built-in tools registered after an agent was saved SHALL appear for that agent automatically unless its `disabled_tools` names them.

#### Scenario: Allowlist gates the surface
- **WHEN** an agent's `disabled_tools` contains only `web.search` and the workspace disables nothing
- **THEN** its executions expose every catalog tool except `web.search`

#### Scenario: Filesystem tools follow the allowlist
- **WHEN** an agent's `disabled_tools` contains `write_file` but not `read_file` or `glob`
- **THEN** its executions expose the read and glob file tools and no write tool

#### Scenario: Empty allowlist exposes nothing
- **WHEN** an agent has an empty `disabled_tools` array and the workspace disables nothing
- **THEN** every registry tool and every filesystem tool is available to its executions

#### Scenario: Facade alias expands
- **WHEN** an agent's `disabled_tools` contains `browser`
- **THEN** its executions expose no browser tool

#### Scenario: Legacy individual browser names still resolve
- **WHEN** an agent's `disabled_tools` contains `browser.navigate` and `browser.read`
- **THEN** the remaining browser tools still resolve

#### Scenario: Workspace gate intersects
- **WHEN** the workspace disables `web.search` and the agent has not disabled it
- **THEN** the agent's executions expose no `web.search` tool

#### Scenario: Unknown names are inert
- **WHEN** an agent is saved with `disabled_tools` naming a tool no registry provides
- **THEN** the save succeeds; the unknown name is inert at execution time

#### Scenario: Later-registered tools do not leak
- **WHEN** a new built-in tool is registered after an agent was saved
- **THEN** the agent's executions expose it unless `disabled_tools` names it

### Requirement: Shell execution
Unless the agent's `disabled_tools` contains the reserved name `execute`, the runtime SHALL expose a shell tool. Shell commands SHALL execute with the agent's workspace directory as the working directory, SHALL inherit a scrubbed minimal environment (no instance secrets), and SHALL be bounded by a timeout and an output cap, with truncation and exit code reported in the tool result. Agents whose `disabled_tools` lists `execute` SHALL have no shell tool. The workspace jail SHALL be understood as a working-directory convention, not an OS sandbox.

#### Scenario: Allow-listed shell runs in the jail
- **WHEN** an agent without `execute` in `disabled_tools` runs a shell command that writes a file
- **THEN** the command executes with the agent's workspace directory as its working directory and the file appears inside the jail

#### Scenario: Shell not allow-listed
- **WHEN** an execution's agent lists `execute` in `disabled_tools`
- **THEN** no shell tool is present on its tool surface

#### Scenario: Timeout bounds the command
- **WHEN** a shell command runs longer than the configured timeout
- **THEN** the command is stopped and the tool result reports the timeout with whatever output was produced
