# workspace-tools Delta

## ADDED Requirements

### Requirement: Tool catalog
The backend SHALL maintain a tool catalog covering every selectable tool: registry built-ins, the filesystem middleware tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), and the reserved shell name `execute`. Each entry SHALL carry a stable key (the allowlist name, with `browser` as the browser facade alias), a human-readable display name, a one-line description, a group, and an icon key. A tool MAY declare itself configurable with a config-field schema (field key, label, type: `secret` | `text` | `number` | `boolean` | `enum`, requirement, help text) so clients can render structured config forms without frontend changes. Registering a new tool into the registry or catalog SHALL be sufficient for it to appear in API responses; no frontend edit SHALL be required.

#### Scenario: Catalog includes filesystem tools
- **WHEN** the catalog is requested
- **THEN** entries include the six filesystem middleware tools alongside registry built-ins and `execute`, each with a display name, description, group, and icon key

#### Scenario: New tool appears without frontend changes
- **WHEN** a new built-in tool is registered with catalog metadata
- **THEN** subsequent catalog responses include it and existing clients render it from metadata alone

### Requirement: Workspace tool settings storage
Each workspace SHALL store per-tool state in a `workspace_tool_settings` table: workspace id, tool key, `enabled` (default true), and `config` (JSON object, default empty). Rows SHALL be workspace-scoped — no query without the workspace boundary. Secret config values SHALL be encrypted at rest with the instance encryption key in the same manner as workspace provider keys, and SHALL never be returned by any endpoint; reads SHALL expose only a non-secret hint (e.g. last four characters) plus all non-secret values.

#### Scenario: Defaults apply without rows
- **WHEN** a workspace has no settings rows for a tool
- **THEN** the tool is treated as enabled with empty config

#### Scenario: Secret never echoed
- **WHEN** settings are read for a tool whose config holds an API key
- **THEN** the response carries a hint derived from the stored key and never the key itself

### Requirement: Tools API
Workspaces SHALL expose `GET /api/v1/workspaces/:ws/tools` returning the catalog merged with workspace state — each entry carrying catalog metadata plus `enabled`, and for configurable tools `configured` and the readable view of its config (non-secret values, secret hints). `PATCH /api/v1/workspaces/:ws/tools/:key` SHALL accept `{enabled?, config?}` and persist the workspace-scoped row. Reading requires workspace membership; patching requires the workspace settings-management permission (Owner/Admin). Patching a key absent from the catalog SHALL be 400 invalid_request; enabling a configurable tool whose required config is missing or invalid SHALL be 422 with the offending fields named.

#### Scenario: Member reads merged view
- **WHEN** a Member GETs the tools endpoint
- **THEN** the response lists every catalog entry with its workspace enabled state and config view

#### Scenario: Admin configures and enables
- **WHEN** an Admin PATCHes `web.search` with a provider and API key, then `{enabled: true}`
- **THEN** the settings persist and subsequent reads show the tool enabled and configured with a key hint

#### Scenario: Enable without required config rejected
- **WHEN** a configurable tool's required fields are unset and a patch sets `enabled: true`
- **THEN** the response is 422 naming the missing fields, and the tool stays disabled

#### Scenario: Member cannot patch
- **WHEN** a Member-role holder PATCHes a tool
- **THEN** response is 403

#### Scenario: Unknown tool key
- **WHEN** a patch addresses a key the catalog does not define
- **THEN** response is 400 invalid_request

### Requirement: Workspace tool gate
Tool resolution for an execution SHALL intersect the agent's allowlist with the workspace's enabled set from tool settings. A workspace-disabled tool SHALL NOT be exposed to any agent in that workspace, and the gate SHALL win over both the agent allowlist and any per-turn allowed-tools override. Facade aliases SHALL expand only from tools that pass the gate — disabling the facade disables every member tool.

#### Scenario: Workspace gate overrides agent allowlist
- **WHEN** `web.search` is disabled for the workspace and an agent's `tools` contains `web.search`
- **THEN** the agent's executions expose no `web.search` tool

#### Scenario: Gate overrides per-turn request
- **WHEN** `web.search` is disabled for the workspace and a per-turn request asks for it
- **THEN** the turn's tool surface still excludes it

#### Scenario: Facade disable cascades
- **WHEN** the workspace disables the `browser` facade
- **THEN** no `browser.*` tool is exposed to any agent in the workspace, regardless of agent allowlists

### Requirement: Search provider configuration
`web.search` SHALL be configurable per workspace through its tool settings: a provider chosen from the provider registry and the credential that provider requires. The registry SHALL ship `duckduckgo` (no credential, default), `tavily`, `brave`, `exa`, `perplexity`, `firecrawl` (API key), and `searxng` (base URL). When a workspace has no provider configured, the resolver SHALL fall back to the instance configuration (environment); when neither exists, `web.search` SHALL use the zero-credential DuckDuckGo backend. The result shape SHALL be identical across providers. `web.search` SHALL start disabled for a workspace until a provider is configured; selecting the key-free DuckDuckGo provider counts as configuring.

#### Scenario: Workspace provider wins
- **WHEN** the workspace configures `tavily` with a key while the instance env selects `duckduckgo`
- **THEN** `web.search` in that workspace resolves through Tavily

#### Scenario: Instance fallback
- **WHEN** the workspace has no search settings and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily

#### Scenario: Disabled until configured
- **WHEN** a fresh workspace has no search provider configured
- **THEN** the tools view reports `web.search` as not configured, the enable attempt is rejected until a provider is saved, and the agent form shows the chip as workspace-disabled

### Requirement: Browser tool configuration
The `browser` facade SHALL be configurable per workspace: `headless` (boolean, default true), `remote_cdp_url` (string, default empty — attach to an existing browser instead of launching), `max_pages` (positive integer, default 1), `idle_timeout_seconds` (positive integer, default 60 — the browser session closes after this much inactivity), and `action_timeout_seconds` (positive integer, default 30 — bounds each browser tool call). When `remote_cdp_url` is set, `headless` SHALL have no effect (the remote browser is already running). Workspace settings SHALL override the instance environment defaults.

#### Scenario: Remote CDP wins over launching
- **WHEN** the workspace sets `remote_cdp_url` and an execution opens a browser session
- **THEN** the session attaches to that endpoint and no local Chromium is launched

#### Scenario: Timeouts bound actions
- **WHEN** a browser tool call exceeds `action_timeout_seconds`
- **THEN** the call stops with a timeout error result and the execution continues

#### Scenario: Idle session torn down
- **WHEN** an execution's browser session is idle beyond `idle_timeout_seconds`
- **THEN** the session is closed and a later browser call starts a fresh session

#### Scenario: Validation ranges enforced
- **WHEN** a patch sets `max_pages` to 0 or a negative timeout
- **THEN** response is 422 naming the offending field
