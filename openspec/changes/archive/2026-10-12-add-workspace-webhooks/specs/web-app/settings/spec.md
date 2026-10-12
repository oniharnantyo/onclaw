# Spec Delta

## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the fourteen sections — Workspace, Providers, Members & roles, Memory, Gateways, Integrations, MCP servers, Skills, Tools, Hooks, Webhooks, API keys, Notifications, and Storage — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. Settings SHALL render full-screen: the icon rail is not rendered on `/settings` routes, and a slim header above the section navigation carries a `← Back` control that returns to the route the user came from (the most recent non-settings route in the current tab, falling back to `/c` when there is none). `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the section nav
- **THEN** the keys section renders with its manage controls and the URL reads /settings/keys

#### Scenario: Gateways reachable
- **WHEN** the user selects "Gateways" in the section nav
- **THEN** the gateways section renders and the URL reads /settings/gateways

#### Scenario: Webhooks reachable
- **WHEN** the user selects "Webhooks" in the section nav
- **THEN** the webhooks section renders and the URL reads /settings/webhooks

#### Scenario: Tools reachable
- **WHEN** the user selects "Tools" in the section nav
- **THEN** the tools section renders and the URL reads /settings/tools

#### Scenario: Deep link
- **WHEN** a member opens /settings/tools directly
- **THEN** the tools section renders without passing through any other section

#### Scenario: Unknown section
- **WHEN** a member opens /settings/nonexistent
- **THEN** the app redirects to /settings/workspace

#### Scenario: Sidebar hidden on settings
- **WHEN** the user is on any /settings route at 1024px width
- **THEN** neither the workspace sidebar nor the icon rail is rendered; the header, section nav column, and section content fill the viewport

#### Scenario: Back returns to origin
- **WHEN** the user opens Settings from the chats view, navigates between sections, and clicks "← Back"
- **THEN** the app returns to the chats view; a deep-linked settings tab with no in-app history falls back to /c
