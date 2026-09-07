# agent-composition Specification

## Purpose

Composes an executable agent from caller-supplied data: a pure build step that turns a complete configuration — identity, instruction, model, tools, and optional capability settings — into a runnable agent, with no access to tenant state.

## Requirements

### Requirement: Pure data-supplied composition
The composition step SHALL construct the agent exclusively from the data supplied in its configuration. It SHALL NOT query stores, the database, or read prompt documents from disk; the instruction arrives as a finished string and the model and tools arrive already built. Composing the same configuration twice SHALL produce equivalent agents.

#### Scenario: Composes without tenant state
- **WHEN** a caller composes an agent passing only configuration data (identity, instruction, model, tools)
- **THEN** composition succeeds without any workspace, agent, user, or provider record being accessed

#### Scenario: Deterministic composition
- **WHEN** the same configuration is composed twice
- **THEN** both results expose the same name, description, instruction, tool surface, and capability wiring

### Requirement: Fail-fast validation
Composition SHALL validate the configuration before constructing anything and SHALL return a descriptive error, constructing no agent, when the model is missing, the name is empty, or the instruction is empty. An iteration cap that is zero or negative SHALL resolve to the package default cap instead of being rejected.

#### Scenario: Missing model rejected
- **WHEN** composition is invoked without a model
- **THEN** an error naming the model is returned and no agent is constructed

#### Scenario: Empty instruction rejected
- **WHEN** composition is invoked with an empty instruction
- **THEN** an error naming the instruction is returned and no agent is constructed

#### Scenario: Iteration cap defaulted
- **WHEN** the configuration's iteration cap is zero
- **THEN** the composed agent uses the package default cap

### Requirement: Selective capability attachment
Each optional capability — filesystem, skills, summarization — SHALL be wired into the composed agent only when its configuration is present; an absent capability contributes nothing to the agent's behavior or tool surface. A configuration with no capabilities present SHALL compose a working agent with none of them attached.

#### Scenario: Filesystem absent removes file tools
- **WHEN** composition runs without a filesystem configuration
- **THEN** no file tools (list, read, write, edit, glob, grep) are present on the composed agent's tool surface

#### Scenario: All capabilities absent
- **WHEN** composition runs with none of the three capability configurations
- **THEN** the composed agent runs with no filesystem, skill, or summarization behavior attached

#### Scenario: Capability present attaches its behavior
- **WHEN** the skills configuration is present
- **THEN** skill discovery and on-demand skill retrieval are attached to the composed agent

### Requirement: Fixed middleware order
When multiple capabilities attach in one composition, their behaviors SHALL be wired in the fixed order: patch-tool-calls, reduction, summarization, skill, filesystem — regardless of the order the configurations appear in.

#### Scenario: Full stack ordering
- **WHEN** a composition includes filesystem, skills, and summarization configurations
- **THEN** the wired behaviors execute in the order patch-tool-calls, reduction, summarization, skill, filesystem

### Requirement: Summarization requires a filesystem target
Summarization offloads conversation history to a file inside the agent's workspace directory. Composition SHALL reject a configuration that enables summarization without a filesystem configuration, since the offload target is undefined.

#### Scenario: Summarization without filesystem rejected
- **WHEN** composition runs with summarization configured and filesystem absent
- **THEN** an error explaining the missing offload target is returned and no agent is constructed

### Requirement: Tool surface as supplied
The business tool surface of the composed agent SHALL be exactly the tools supplied in the configuration. Composition SHALL NOT consult a tool registry or apply denylist filtering; such resolution belongs to the caller.

#### Scenario: Supplied tools are the surface
- **WHEN** a caller supplies a specific set of tools
- **THEN** exactly those tools are invocable by the composed agent, and no others are added from any registry
