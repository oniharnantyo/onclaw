# workspace-tools — Delta

## MODIFIED Requirements

### Requirement: Tool catalog
The backend SHALL maintain a tool catalog covering every selectable tool: registry built-ins (including the todo tools `todo_write` and `todo_read` and the generative-UI echo tools `ui.chart`, `ui.timeline`, and `ui.preview`), the filesystem middleware tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), the `document.read` tool, the reserved shell name `execute`, and the channel toolset (`channel.post`, `channel.history`, `session.close`). Each entry SHALL carry a stable key (the allowlist name, with `browser` as the browser facade alias), a human-readable display name, a one-line description, a group, and an icon key. Each entry SHALL also carry a toggleability marker: the three channel toolset entries SHALL be marked non-toggleable (always-on in their execution context), and every other entry SHALL be marked toggleable. A tool MAY declare itself configurable with a config-field schema (field key, label, type: `secret` | `text` | `number` | `boolean` | `enum`, requirement, help text) so clients can render structured config forms without frontend changes. Registering a new tool into the registry or catalog SHALL be sufficient for it to appear in API responses; no frontend edit SHALL be required.

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

#### Scenario: Todo and generative-UI tools are catalog entries
- **WHEN** the catalog is requested
- **THEN** entries include `todo_write`, `todo_read`, `ui.chart`, `ui.timeline`, and `ui.preview` with display name, description, group, and icon key, each toggleable and selectable per agent
