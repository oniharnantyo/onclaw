## MODIFIED Requirements

### Requirement: Tool catalog
The backend SHALL maintain a tool catalog covering every selectable tool: registry built-ins, the filesystem middleware tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), the `document.read` tool, and the reserved shell name `execute`. Each entry SHALL carry a stable key (the allowlist name, with `browser` as the browser facade alias), a human-readable display name, a one-line description, a group, and an icon key. A tool MAY declare itself configurable with a config-field schema (field key, label, type: `secret` | `text` | `number` | `boolean` | `enum`, requirement, help text) so clients can render structured config forms without frontend changes. Registering a new tool into the registry or catalog SHALL be sufficient for it to appear in API responses; no frontend edit SHALL be required.

#### Scenario: Catalog includes filesystem tools
- **WHEN** the catalog is requested
- **THEN** entries include the six filesystem middleware tools alongside registry built-ins and `execute`, each with a display name, description, group, and icon key

#### Scenario: New tool appears without frontend changes
- **WHEN** a new built-in tool is registered with catalog metadata
- **THEN** subsequent catalog responses include it and existing clients render it from metadata alone

#### Scenario: Catalog includes document.read
- **WHEN** the catalog is requested
- **THEN** entries include `document.read` with a display name, description, group, and icon key, selectable per agent and subject to the workspace tool gate
