# Spec Delta

## Purpose

Give agents real tool execution: turns invoke the built-in tool catalog — jailed workspace filesystem, shell, todos, and web access — and feed results back to the model until the turn concludes.

## ADDED Requirements

### Requirement: Tool execution loop

A turn SHALL resolve the agent's tool surface, offer it to the model, execute the tool calls the model makes, feed each result back, and continue until the model answers without tool calls or an iteration cap is reached. Each tool call and its result SHALL be recorded on the session transcript as it happens, with the result content persisted. When the iteration cap is reached, the turn SHALL end with a visible notice instead of failing silently.

#### Scenario: Tool call round trip

- **WHEN** the model calls a tool mid-turn
- **THEN** the tool executes, the result is fed back to the model, and the turn continues toward a final answer

#### Scenario: Runaway loop is bounded

- **WHEN** a turn's tool-call iterations exceed the cap
- **THEN** the turn ends with a visible cap notice rather than continuing indefinitely

#### Scenario: Tool failure feeds back

- **WHEN** a tool invocation fails
- **THEN** the error is returned to the model as the tool result and the turn continues

### Requirement: Tool gate

A built-in tool SHALL be offered to a turn only when it is in the catalog, enabled globally in the Tools pane, and permitted by the agent's tool allowlist (an empty allowlist permits all catalog tools). MCP tools are gated separately by their own capability.

#### Scenario: Globally disabled tool is not offered

- **WHEN** a tool is disabled in the Tools pane
- **THEN** subsequent turns of every agent are offered no such tool

#### Scenario: Allowlist narrows the surface

- **WHEN** an agent's tool allowlist lists a subset of tools
- **THEN** only that subset is offered on its turns

### Requirement: Workspace jail

The filesystem tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, `delete_file`) SHALL operate only inside the agent's workspace directory, presented to the model as its workspace root. Paths resolving outside the jail SHALL return an error result, not end the turn.

#### Scenario: File edits stay in the workspace

- **WHEN** the model writes and then reads a file in the workspace
- **THEN** the operations succeed against the agent's workspace directory on disk

#### Scenario: Escape attempt is rejected

- **WHEN** the model requests a path outside the workspace
- **THEN** the tool returns an out-of-scope error result and the turn continues

### Requirement: Jailed shell

The `execute` tool SHALL run shell commands with the workspace as the working directory, a bounded timeout, and a bounded output size; the child environment SHALL NOT leak the host app's credentials. Timeouts and output truncation SHALL be reported in the tool result.

#### Scenario: Command output is captured

- **WHEN** the model runs a shell command
- **THEN** the combined output within the size cap is returned as the tool result

#### Scenario: Hanging command is killed

- **WHEN** a command exceeds the timeout
- **THEN** the process is killed and the tool result reports the timeout

### Requirement: Todo tools

The `todo_write` and `todo_read` tools SHALL let the agent maintain a task plan that persists in the agent's workspace across turns.

#### Scenario: Plan persists across turns

- **WHEN** the agent writes a plan in one turn and reads it in a later turn
- **THEN** the same plan is returned

### Requirement: Web fetch

The `web_fetch` tool SHALL retrieve a URL and return its content as readable text. Fetch failures SHALL be returned as tool results.

#### Scenario: Page is fetched

- **WHEN** the model fetches a reachable URL
- **THEN** the tool result contains the page's textual content

#### Scenario: Unreachable URL

- **WHEN** the model fetches a URL that cannot be reached
- **THEN** the tool result reports the failure and the turn continues

### Requirement: Web search

The `web_search` tool SHALL resolve queries through a configurable provider chain — brave, exa, perplexity, and firecrawl by API key, searxng by base URL — trying providers in order and failing over on error. With no provider configured, or when every provider in the chain fails, the tool SHALL return an explanatory error result.

#### Scenario: Search returns results

- **WHEN** the model searches with a configured provider
- **THEN** the tool result lists organic results (title, URL, snippet)

#### Scenario: Failover to the next provider

- **WHEN** the first provider in the chain fails
- **THEN** the next configured provider is tried and its results are returned

#### Scenario: No provider configured

- **WHEN** web_search runs with no provider configured
- **THEN** the tool result explains that a search provider must be configured in Tools settings
