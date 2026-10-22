# Spec Delta

## MODIFIED Requirements

### Requirement: Tools pane

The settings menu SHALL list the available tools with their descriptions and SHALL allow enabling or disabling each tool. Disabled tools MUST NOT be offered to agents. The pane SHALL also configure the web_search provider chain: providers are chosen from the catalog (brave, exa, perplexity, and firecrawl, each requiring an API key; searxng requiring a base URL), ordered into a failover chain, and persisted for the runtime. Saving a chain provider without its required credential SHALL be rejected.

#### Scenario: Toggle a tool

- **WHEN** the user disables a tool that an agent had enabled
- **THEN** the agent's subsequent turns no longer offer that tool

#### Scenario: Configure the search chain

- **WHEN** the user adds brave with an API key and then searxng with a base URL
- **THEN** the chain is saved in that order and web_search tries brave before searxng

#### Scenario: Missing credential is rejected

- **WHEN** the user adds a chain provider that requires an API key and submits without one
- **THEN** the form rejects the submission and explains which credential is missing

### Requirement: Hooks pane

The settings menu SHALL let the user view, add, edit, and remove global hooks — each bound to one of the supported events with a command or script handler type (other handler types are not offered) — and SHALL provide a test affordance that exercises the hook with a sample event and reports the outcome. Per-agent hook assignment lives in agent configuration, not here.

#### Scenario: Test a hook

- **WHEN** the user runs the test action on a command hook
- **THEN** the hook executes against a sample event and its outcome (success or failure) is displayed

#### Scenario: Only supported handler types are offered

- **WHEN** the user opens the add/edit hook form
- **THEN** the handler-type choices are command and script only

### Requirement: MCP pane

The settings menu SHALL let the user add, edit, and remove MCP server definitions (transport, command or URL, arguments, environment) and SHALL show each server's live connection state — probed on demand, showing connected with the discovered tool count or failed with the reason. Every enabled server is available to agents by default; agents restrict the set from their own configuration.

#### Scenario: Add an MCP server

- **WHEN** the user adds a valid MCP server definition
- **THEN** the server is listed with its connection state and becomes available to every agent that has not restricted its server list

#### Scenario: Probe shows live state

- **WHEN** the user probes a reachable server
- **THEN** its row shows connected with the discovered tool count

#### Scenario: Failed probe shows the reason

- **WHEN** the user probes a server that cannot be reached
- **THEN** its row shows the failed state with the underlying reason and the stored definition is unchanged
