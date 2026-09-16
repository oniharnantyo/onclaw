## MODIFIED Requirements

### Requirement: Gateways pane
The Gateways pane SHALL present gateway platforms through a second sidebar inside the pane: a section per platform (Telegram, WhatsApp) holding one status-bearing row per gateway account, with the selected platform's configuration rendered in a detail pane to the right. Each row SHALL show the platform icon, an identity second line (@bot_username for Telegram; the linked lane identity for WhatsApp), and a status dot drawn from a shared vocabulary — Connected, Paused, Error, Not set up (WhatsApp linked state: Linked) — kept in sync with the detail pane's status card. At widths <768px the sidebar SHALL collapse into a horizontal scrollable chip row carrying the same dots, with the detail stacked beneath; the 360×800 minimum viewport SHALL not overflow horizontally.

The pane SHALL widen from the single `max-w-xl` column to the full settings content area (sidebar ~208px + flexing detail). Platform selection SHALL be local component state; sections SHALL NOT add URL routes. All existing management surfaces persist inside the detail pane per platform: connect a bot by pasting a BotFather token (write-only field, stored encrypted, shown back as a secret hint), see the resolved bot username and status, set the default agent for direct messages, choose the transport mode, enable/disable the gateway, and manage bound Telegram groups (bind by in-chat binding command, list with the bound agent, unlink). Member-facing, each platform's detail exposes the personal pairing flow: generate a one-time pairing token shown as a copyable command with an expiry countdown, and show/unlink the current link for the signed-in member. Guard rejections (non-admin on admin actions, invalid tokens, one-binding-per-group conflicts) surface as toasts. The pane SHALL replace the mock Telegram entry in the legacy integrations list with the real surface.

#### Scenario: Sidebar shows platform status
- **WHEN** an admin opens the Gateways pane with Telegram connected and WhatsApp not set up
- **THEN** the sidebar lists a Telegram row with a Connected dot and a WhatsApp row with a Not set up dot, and the detail pane renders the selected platform

#### Scenario: Selecting a platform switches the detail
- **WHEN** the user selects the WhatsApp row in the sidebar
- **THEN** the detail pane renders the WhatsApp configuration surface and the Telegram row remains visible with its status dot unchanged

#### Scenario: Sidebar collapses on small viewports
- **WHEN** the viewport is narrower than 768px
- **THEN** the platform rows render as a horizontal scrollable chip row above the stacked detail, carrying the same status dots, with no horizontal page overflow at 360px

#### Scenario: Status reflects a paused gateway
- **WHEN** a configured gateway is disabled
- **THEN** its sidebar row dot reads Paused and the detail status card agrees

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
- **THEN** a one-time pairing command is shown with a live expiry countdown, and after pairing completes the pane shows their linked identity with an unlink control

#### Scenario: Non-admin sees read-only
- **WHEN** a Member opens the Gateways pane
- **THEN** the admin controls (bot connection, default agent, bindings, enable/disable) are hidden or disabled, and only the personal pairing flow is offered
