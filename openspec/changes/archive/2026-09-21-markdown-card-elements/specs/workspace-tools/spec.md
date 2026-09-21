## MODIFIED Requirements

### Requirement: Tool catalog
The backend SHALL maintain a tool catalog covering every selectable tool: registry built-ins, the filesystem middleware tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), the `document.read` tool, the reserved shell name `execute`, and the channel toolset (`channel.post`, `channel.history`, `session.close`). The catalog SHALL NOT offer the generative-UI echo tools `ui.chart`, `ui.timeline`, or `ui.preview`: rich card rendering is prompt-guided markdown fences (the `web-app/generative-ui` capability), not tool calls, and any stored agent allowlists naming the removed keys SHALL be inert. Each entry SHALL carry a stable key (the allowlist name, with `browser` as the browser facade alias), a human-readable display name, a one-line description, a group, and an icon key. Each entry SHALL also carry a toggleability marker: the three channel toolset entries SHALL be marked non-toggleable (always-on in their execution context), and every other entry SHALL be marked toggleable. A tool MAY declare itself configurable with a config-field schema (field key, label, type: `secret` | `text` | `number` | `boolean` | `enum`, requirement, help text) so clients can render structured config forms without frontend changes. Registering a new tool into the registry or catalog SHALL be sufficient for it to appear in API responses; no frontend edit SHALL be required.

#### Scenario: Catalog includes filesystem tools
- **WHEN** the catalog is requested
- **THEN** entries include the six filesystem middleware tools alongside registry built-ins and `execute`, each with a display name, description, group, and icon key

#### Scenario: Echo tools are not offered
- **WHEN** the catalog is requested
- **THEN** no entry exists for `ui.chart`, `ui.timeline`, or `ui.preview`, and workspace tool settings referencing those keys are ignored rather than served

#### Scenario: Todo and generative-UI tools are catalog entries
- **WHEN** the catalog is requested
- **THEN** entries include `todo_write` and `todo_read` with display name, description, group, and icon key, each toggleable and selectable per agent, and the removed echo tools appear as no entries at all

#### Scenario: Stale allowlist keys are inert
- **WHEN** an agent's stored tool allowlist still names `ui.chart`
- **THEN** agent creation, update, and execution succeed; the unknown key contributes no tool to runs

#### Scenario: New tool appears without frontend changes
- **WHEN** a new built-in tool is registered with catalog metadata
- **THEN** subsequent catalog responses include it and existing clients render it from metadata alone

#### Scenario: Catalog includes document.read
- **WHEN** the catalog is requested
- **THEN** entries include `document.read` with a display name, description, group, and icon key, selectable per agent and subject to the workspace tool gate

#### Scenario: Channel toolset marked non-toggleable
- **WHEN** the catalog is requested
- **THEN** `channel.post`, `channel.history`, and `session.close` appear with display name, description, group, and icon key and carry the non-toggleable marker, and no other entry carries it
