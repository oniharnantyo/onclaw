## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL present eight sections — Workspace, Providers, Members & roles, Integrations, MCP servers, Skills, API keys, Notifications — as a tabbed rail inside one modal, each pane reachable without reloading.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the tab rail
- **THEN** the keys pane renders with its manage controls

#### Scenario: Providers reachable
- **WHEN** the user selects "Providers" in the tab rail
- **THEN** the providers pane renders, listing the workspace's provider configs

## ADDED Requirements

### Requirement: Providers pane
The providers pane SHALL list the workspace's provider configs from the API — type badge, name, base URL (for `-compatible` types and overrides), key-set state with the key hint (last 4), and an enabled toggle. Owners and Admins create configs via a structured form (type select from the six catalog types, name, base URL shown only when the type requires or overrides it, password-style key input); the key input is write-only — existing keys are never echoed back, replaced only. Delete SHALL require confirmation. A per-row verify action SHALL report its outcome transiently (inline banner or toast) — never persisted, gone on unmount. Guard rejections (missing permission) SHALL surface as toasts. The pane SHALL show an inviting empty state when no providers are configured.

#### Scenario: Create an OpenAI config
- **WHEN** an Owner submits type "OpenAI", name "Acme prod", key
- **THEN** the row appears with type badge OpenAI, key set "…last4", enabled on, and a creation toast

#### Scenario: Compatible type form
- **WHEN** the user selects type "openai-compatible" in the create form
- **THEN** the base URL field becomes required and help text explains the origin rule (no version path)

#### Scenario: Key never echoed
- **WHEN** the user edits "Acme prod" without touching the key field
- **THEN** the form never displays the stored key, and PATCH preserves it

#### Scenario: Verify transient result
- **WHEN** the user clicks verify on "Acme prod"
- **THEN** a busy state shows, then an inline banner reports success or the provider error; the row data is unchanged

#### Scenario: Empty state
- **WHEN** the workspace has no provider configs
- **THEN** the pane shows an empty state inviting configuration, visible to Members too (read is a permission)
