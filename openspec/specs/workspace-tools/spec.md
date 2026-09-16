# workspace-tools Specification

## Purpose

The per-workspace tool catalog and its control surface: what every selectable tool is, whether the workspace has it enabled, and its structured configuration — exposed through the tools API and enforced as the gate that wins over agent allowlists at execution time.

## Requirements

### Requirement: Tool catalog
The backend SHALL maintain a tool catalog covering every selectable tool: registry built-ins, the filesystem middleware tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), the `document.read` tool, the reserved shell name `execute`, and the channel toolset (`channel.post`, `channel.history`, `session.close`). Each entry SHALL carry a stable key (the allowlist name, with `browser` as the browser facade alias), a human-readable display name, a one-line description, a group, and an icon key. Each entry SHALL also carry a toggleability marker: the three channel toolset entries SHALL be marked non-toggleable (always-on in their execution context), and every other entry SHALL be marked toggleable. A tool MAY declare itself configurable with a config-field schema (field key, label, type: `secret` | `text` | `number` | `boolean` | `enum`, requirement, help text) so clients can render structured config forms without frontend changes. Registering a new tool into the registry or catalog SHALL be sufficient for it to appear in API responses; no frontend edit SHALL be required.

#### Scenario: Catalog includes filesystem tools
- **WHEN** the catalog is requested
- **THEN** entries include the six filesystem middleware tools alongside registry built-ins and `execute`, each with a display name, description, group, and icon key

#### Scenario: New tool appears without frontend changes
- **WHEN** a new built-in tool is registered with catalog metadata
- **THEN** subsequent catalog responses include it and existing clients render it from metadata alone

#### Scenario: Catalog includes document.read
- **WHEN** the catalog is requested
- **THEN** entries include `document.read` with a display name, description, group, and icon key, selectable per agent and subject to the workspace tool gate

#### Scenario: Channel toolset marked non-toggleable
- **WHEN** the catalog is requested
- **THEN** `channel.post`, `channel.history`, and `session.close` appear with display name, description, group, and icon key and carry the non-toggleable marker, and no other entry carries it

### Requirement: Workspace tool settings storage
Each workspace SHALL store per-tool state in a `workspace_tool_settings` table: workspace id, tool key, `enabled` (default true), and `config` (JSON object, default empty). Rows SHALL be workspace-scoped — no query without the workspace boundary. Secret config values SHALL be encrypted at rest with the instance encryption key in the same manner as workspace provider keys, and SHALL never be returned by any endpoint; reads SHALL expose only a non-secret hint (e.g. last four characters) plus all non-secret values.

#### Scenario: Defaults apply without rows
- **WHEN** a workspace has no settings rows for a tool
- **THEN** the tool is treated as enabled with empty config

#### Scenario: Secret never echoed
- **WHEN** settings are read for a tool whose config holds an API key
- **THEN** the response carries a hint derived from the stored key and never the key itself

### Requirement: Tools API
Workspaces SHALL expose `GET /api/v1/workspaces/:ws/tools` returning the catalog merged with workspace state — each entry carrying catalog metadata plus `enabled` and the toggleability marker, and for configurable tools `configured` and the readable view of its config (non-secret values, secret hints). The view SHALL report non-toggleable entries as enabled regardless of any stored settings row. `PATCH /api/v1/workspaces/:ws/tools/:key` SHALL accept `{enabled?, config?}` and persist the workspace-scoped row; a patch setting `enabled` on a non-toggleable entry SHALL be rejected with 422 and leave the stored state unchanged. Reading requires workspace membership; patching requires the workspace settings-management permission (Owner/Admin). Patching a key absent from the catalog SHALL be 400 invalid_request; enabling a configurable tool whose required config is missing or invalid SHALL be 422 with the offending fields named.

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

#### Scenario: Non-toggleable entries read as enabled
- **WHEN** any member GETs the tools endpoint
- **THEN** `channel.post`, `channel.history`, and `session.close` carry the non-toggleable marker and read as enabled, even if a stored settings row says otherwise

#### Scenario: Enabled patch on non-toggleable rejected
- **WHEN** an Admin PATCHes `channel.post` with `{enabled: false}`
- **THEN** the response is 422 and the stored settings are unchanged

### Requirement: Workspace tool gate
Tool resolution for an execution SHALL intersect the agent's allowlist with the workspace's enabled set from tool settings. A workspace-disabled tool SHALL NOT be exposed to any agent in that workspace, and the gate SHALL win over both the agent allowlist and any per-turn allowed-tools override. Facade aliases SHALL expand only from tools that pass the gate — disabling the facade disables every member tool. Non-toggleable (always-on) tools SHALL be exempt from the enabled-set intersection: workspace settings SHALL NOT remove them, stored disabled rows for them SHALL be ignored, and their exposure SHALL remain governed solely by the run's execution context (channel-run scoping; the facilitator and active-session conditions for `session.close`).

#### Scenario: Workspace gate overrides agent allowlist
- **WHEN** `web.search` is disabled for the workspace and an agent's `tools` contains `web.search`
- **THEN** the agent's executions expose no `web.search` tool

#### Scenario: Gate overrides per-turn request
- **WHEN** `web.search` is disabled for the workspace and a per-turn request asks for it
- **THEN** the turn's tool surface still excludes it

#### Scenario: Facade disable cascades
- **WHEN** the workspace disables the `browser` facade
- **THEN** no `browser.*` tool is exposed to any agent in the workspace, regardless of agent allowlists

#### Scenario: Stale disabled row does not strip an always-on tool
- **WHEN** the workspace holds an `enabled=false` settings row for `channel.history` and a channel run resolves its toolset
- **THEN** `channel.history` is still exposed to that run

### Requirement: Search provider configuration
`web.search` SHALL be configurable per workspace through its tool settings as an ordered list of named provider entries: each entry carries a unique non-empty name, a provider chosen from the provider registry, and the credential that provider requires. The registry SHALL ship `tavily`, `brave`, `exa`, `perplexity`, and `firecrawl` (API key) and `searxng` (base URL); a credential-free scraping provider SHALL NOT exist. The same provider MAY appear in multiple entries with different credentials. A workspace may store any number of entries (bounded by a platform cap well above the request window); list order is priority. Request resolution SHALL build a failover chain from the first three entries in list order (a positional window; the window size is a platform constant): each attempt is bounded by the tool's per-attempt request timeout (`request_timeout_seconds`, positive integer, default 10, maximum 60); any attempt error — non-200 status, network failure, or response decode failure — SHALL advance to the next entry in the window; a successful response with zero results SHALL be returned as a valid answer without advancing; when every entry in the window fails, the request SHALL fail with the last error prefixed by the failing entry's name. Entries below the window SHALL never serve while they sit below it. When a workspace has no entries, the resolver SHALL fall back to the instance configuration (environment) as a single-entry chain; when neither entries nor a usable env provider exist, `web.search` SHALL fail construction with an explicit "not configured" error and SHALL NOT fall back to any credential-free backend. Enabling `web.search` SHALL require at least one fully valid entry (unique non-empty name, known provider, credential present per kind); enabling with none, or saving duplicate entry names, SHALL be rejected with a 422 naming the offending entry. The result shape SHALL be identical across providers.

#### Scenario: Workspace provider wins
- **WHEN** the workspace configures entries ("Tavily 1", "Tavily 2", "Exa 1") while the instance env selects a different provider
- **THEN** `web.search` resolves through the workspace's entry chain in list order

#### Scenario: Multiple keys of one provider
- **WHEN** the workspace configures "Tavily 1" (key A), "Tavily 2" (key B), and "Exa 1" (key C) and Tavily 1 returns a 429
- **THEN** the request advances through Tavily 2 and answers from the first successful entry without surfacing the 429

#### Scenario: Window is positional
- **WHEN** five entries are stored and the first three all fail
- **THEN** the request fails naming the third entry, and the fourth and fifth entries never serve while they sit below the window

#### Scenario: Reorder promotes a standby entry
- **WHEN** the user moves the fifth entry into the top three and saves
- **THEN** subsequent requests try that entry inside the window

#### Scenario: Empty result set is a valid answer
- **WHEN** the first entry responds 200 with zero results
- **THEN** the empty result is returned without advancing to the next entry

#### Scenario: Per-attempt timeout advances the chain
- **WHEN** an entry exceeds `request_timeout_seconds`
- **THEN** the attempt is abandoned and the next entry in the window is tried

#### Scenario: Unconfigured fails fast
- **WHEN** a workspace has no entries and the instance env names no usable provider
- **THEN** `web.search` fails construction with an explicit "not configured" error before any network request

#### Scenario: Instance fallback
- **WHEN** the workspace has no entries and the instance env selects `tavily` with a key
- **THEN** `web.search` resolves through Tavily as a single-entry chain

#### Scenario: Env provider without credential errors
- **WHEN** the env selects a key-requiring provider and its credential is absent
- **THEN** `web.search` fails construction naming the missing credential

#### Scenario: Disabled until configured
- **WHEN** a fresh workspace has no search entries configured
- **THEN** the tools view reports `web.search` as not configured and the enable attempt is rejected until a valid entry is saved

#### Scenario: Enable gating and unique names
- **WHEN** a save enables `web.search` with zero valid entries, or stores two entries named "Tavily 1"
- **THEN** the response is a 422 naming the offending entry and the stored settings are unchanged

#### Scenario: Flat settings migrated
- **WHEN** migration 000023 runs on a row storing the flat `{provider: tavily, api_key}` shape
- **THEN** the row becomes a single auto-named entry carrying the same credential envelope, and a row selecting `duckduckgo` becomes an empty entries list (explicitly unconfigured)
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
