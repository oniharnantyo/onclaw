# agent-runtime Delta

Stacks on the un-archived `enrich-agent-tools` change: the requirements below restate that change's "Tool selection", "Web search tool providers", and "Browser automation" texts as modified by this change. Archive `enrich-agent-tools` first.

## MODIFIED Requirements

### Requirement: Tool selection
An agent's tool surface SHALL be resolved from its `tools` allowlist intersected with the workspace's enabled tool set (see the workspace-tools capability — the workspace gate wins over the allowlist and over per-turn allowed-tools overrides). A name listed in `tools` SHALL expose the corresponding registered built-in tool. The reserved name `execute` SHALL enable the shell tool (see "Shell execution"). The facade alias `browser` SHALL expand to the full browser tool set at resolution (see "Browser automation"); individual `browser.*` names in an allowlist SHALL continue to resolve for backward compatibility. The filesystem middleware tools SHALL be selectable through the allowlist names `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`: a name present SHALL keep that middleware tool attached, a name absent SHALL disable it via the filesystem middleware's per-tool disable configuration. An agent whose `tools` array is empty SHALL have no registry tools and no filesystem tools exposed. Names in `tools` that match no registered tool SHALL be ignored, not errors. Built-in tools registered after an agent's allowlist was saved SHALL NOT appear for that agent until its allowlist is updated.

#### Scenario: Allowlist gates the surface
- **WHEN** an agent's `tools` contains only `web.search` and the workspace enables it
- **THEN** its executions expose `web.search` and no other registry tool

#### Scenario: Filesystem tools follow the allowlist
- **WHEN** an agent's `tools` contains `read_file` and `glob` but not `write_file`
- **THEN** its executions expose the read and glob file tools and no write tool

#### Scenario: Empty allowlist exposes nothing
- **WHEN** an agent has an empty `tools` array
- **THEN** no registry tool and no filesystem tool is available to its executions

#### Scenario: Facade alias expands
- **WHEN** an agent's `tools` contains `browser` and the workspace enables it
- **THEN** its executions expose the full current browser tool set

#### Scenario: Legacy individual browser names still resolve
- **WHEN** an existing agent's `tools` contains `browser.navigate` and `browser.read`
- **THEN** exactly those tools resolve until the allowlist is updated

#### Scenario: Workspace gate intersects
- **WHEN** the workspace disables `web.search` and an agent's allowlist contains it
- **THEN** the agent's executions expose no `web.search` tool

#### Scenario: Unknown names are inert
- **WHEN** an agent is saved with `tools` naming a tool no registry provides
- **THEN** the save succeeds; the unknown name is inert at execution time

#### Scenario: Later-registered tools do not leak
- **WHEN** a new built-in tool is registered after an agent's allowlist was saved
- **THEN** the agent's executions do not expose it until the allowlist names it

### Requirement: Web search tool providers
The `web.search` tool SHALL resolve queries through a search provider selected per workspace from the workspace's tool settings, falling back to instance configuration when the workspace has none, and to the zero-credential DuckDuckGo backend when neither exists (see the workspace-tools capability, "Search provider configuration"). The provider registry SHALL be extensible: a provider registers the credential kind it requires (`none`, `api_key`, `base_url`) and the catalog uses this to render configuration. The result shape SHALL be identical across providers. A workspace-selected provider whose credential is missing SHALL fail the tool's construction for the execution with an error naming the missing configuration, not silently fall back.

#### Scenario: Workspace provider selected
- **WHEN** the workspace configures `brave` with an API key
- **THEN** `web.search` in that workspace resolves through Brave in the standard result shape

#### Scenario: Instance fallback preserved
- **WHEN** the workspace has no search settings and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily

#### Scenario: Zero-credential default
- **WHEN** neither workspace nor instance configuration exists
- **THEN** `web.search` uses the DuckDuckGo backend

#### Scenario: Misconfigured workspace provider fails construction
- **WHEN** the workspace selects `tavily` without a stored key
- **THEN** the tool construction for the execution fails with an error naming the missing configuration

### Requirement: Browser automation
The runtime SHALL expose a browser tool set behind the `browser` facade: `browser.navigate`, `browser.snapshot`, `browser.click`, `browser.type`, `browser.hover`, `browser.drag`, `browser.select_option`, `browser.act`, `browser.read`, and `browser.screenshot`. The set SHALL follow the accessibility-snapshot model: `browser.snapshot` SHALL capture a page snapshot whose elements carry stable references, and the interaction tools (`click`, `type`, `hover`, `drag`, `select_option`) SHALL target elements by those references. The runtime SHALL attach to the workspace-configured remote CDP endpoint when one is provided, otherwise launch a local Chromium under the workspace-configured headless setting, honoring `max_pages`, `idle_timeout_seconds`, and `action_timeout_seconds` from the workspace's tool settings (see the workspace-tools capability, "Browser tool configuration"). When no browser is available, the tools SHALL return a clear availability error instead of failing the run. Each execution SHALL get its own isolated browser session, torn down when the execution ends or the idle timeout expires. Screenshots SHALL be written into the agent's workspace directory, and the tool result SHALL return the in-jail path.

#### Scenario: Snapshot then ref-targeted interaction
- **WHEN** the tools take a snapshot and then click an element by its snapshot reference
- **THEN** `browser.click` operates on that element and returns a fresh snapshot in its result

#### Scenario: Navigate and read a page
- **WHEN** the tools navigate to a URL and then read the page
- **THEN** `browser.navigate` reports the destination (title/status) and `browser.read` returns the page's readable content

#### Scenario: Session is per-execution
- **WHEN** an execution ends
- **THEN** its browser session and any launched browser process are stopped, and a later execution starts a fresh session

#### Scenario: Screenshot lands in the workspace
- **WHEN** `browser.screenshot` captures the current page
- **THEN** the PNG is written inside the agent's workspace directory and the result returns that path

#### Scenario: No browser available
- **WHEN** no CDP endpoint is configured and no local Chromium can be found
- **THEN** browser tool calls return an availability error describing the configuration needed
