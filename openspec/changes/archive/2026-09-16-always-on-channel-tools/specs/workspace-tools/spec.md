## MODIFIED Requirements

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
