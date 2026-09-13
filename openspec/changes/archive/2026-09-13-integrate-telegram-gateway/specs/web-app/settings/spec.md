## MODIFIED Requirements

### Requirement: Settings navigation
Settings SHALL be the routed page surface `/settings/:section` presenting the ten sections — Workspace, Providers, Members & roles, Gateways, MCP servers, Skills, Tools, API keys, Notifications, and Storage — with its own section navigation (a static left column at widths ≥768px, horizontal scroll tabs below) in place of the workspace sidebar. `/settings` SHALL redirect to the Workspace section. The active section SHALL be carried in the URL so sections are deep-linkable and browser back/forward move between sections.

#### Scenario: Switch panes
- **WHEN** the user selects "API keys" in the section nav
- **THEN** the keys section renders with its manage controls and the URL reads /settings/keys

#### Scenario: Gateways reachable
- **WHEN** the user selects "Gateways" in the section nav
- **THEN** the gateways section renders and the URL reads /settings/gateways

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
- **THEN** the workspace sidebar is not rendered; the section nav column and section content fill the area

## ADDED Requirements

### Requirement: Gateways pane
The Gateways pane SHALL let Owners and Admins manage the workspace's Telegram gateway from the real API: connect a bot by pasting a BotFather token (write-only field, stored encrypted, shown back as a secret hint), see the resolved bot username and a connected/disabled status, set the default agent for direct messages via the agent picker, choose the transport mode (webhook URL display or long-polling), enable/disable the gateway, and manage bound Telegram groups (bind by in-chat binding command, list with the bound agent, unlink). Member-facing, the pane exposes the personal pairing flow: generate a one-time pairing token shown as a copyable `/start <token>` command with an expiry countdown, and show/unlink the current Telegram link for the signed-in member. Guard rejections (non-admin on admin actions, invalid tokens, one-binding-per-group conflicts) surface as toasts. The pane SHALL replace the mock Telegram entry in the legacy integrations list with the real surface.

#### Scenario: Admin connects a bot
- **WHEN** an Owner pastes a valid BotFather token and saves
- **THEN** the pane shows the gateway as connected with the bot username resolved from Telegram, and the token is never displayed again

#### Scenario: Default agent picker
- **WHEN** an admin opens the default-agent control
- **THEN** the workspace's real agents are offered, and the saved choice drives which agent answers untargeted direct messages

#### Scenario: Group binding listed
- **WHEN** a bound group's binding command is confirmed in Telegram
- **THEN** the pane lists that group with its bound agent and an unlink control

#### Scenario: Member pairs from the pane
- **WHEN** a signed-in member opens the pairing flow
- **THEN** a one-time `/start <token>` command is shown with a live expiry countdown, and after pairing completes the pane shows their linked Telegram identity with an unlink control

#### Scenario: Non-admin sees read-only
- **WHEN** a Member opens the Gateways pane
- **THEN** the admin controls (bot connection, default agent, bindings, enable/disable) are hidden or disabled, and only the personal pairing flow is offered
