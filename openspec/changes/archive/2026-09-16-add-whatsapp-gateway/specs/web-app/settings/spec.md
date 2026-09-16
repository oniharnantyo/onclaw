## MODIFIED Requirements

### Requirement: Gateways pane
The Gateways pane SHALL let Owners and Admins manage the workspace's gateway platforms from the real API. The Telegram section SHALL keep its existing surface: connect a bot by pasting a BotFather token (write-only field, stored encrypted, shown back as a secret hint), see the resolved bot username and a connected/disabled status, set the default agent for direct messages via the agent picker, choose the transport mode (webhook URL display or long-polling), enable/disable the gateway, and manage bound Telegram groups (bind by in-chat binding command, list with the bound agent, unlink). The WhatsApp section SHALL offer lane selection followed by lane-appropriate configuration as labeled fields (never a raw JSON textarea): the cloud lane presents access token, phone number id, app secret, and webhook verify token as write-only fields, the webhook callback URL to paste into the Meta dashboard with its verify token, and a connected status; the multi-device lane presents the pairing flow — start pairing, scan the displayed QR code or enter the displayed 8-digit pair code, live connection status, and logout — preceded by a clear account-ban risk notice. Both WhatsApp lanes share the default-agent picker and enable/disable control. Member-facing, the pane exposes the personal pairing flow per platform: generate a one-time pairing token shown as a copyable `/start <token>` command with an expiry countdown, and show/unlink the current platform link for the signed-in member. Guard rejections (non-admin on admin actions, invalid credentials, lane validation errors) surface as toasts.

#### Scenario: Admin connects a bot
- **WHEN** an Owner pastes a valid BotFather token and saves
- **THEN** the pane shows the Telegram gateway as connected with the bot username resolved from Telegram, and the token is never displayed again

#### Scenario: Default agent picker
- **WHEN** an admin opens the default-agent control
- **THEN** the workspace's real agents are offered, and the saved choice drives which agent answers untargeted direct messages

#### Scenario: Group binding listed
- **WHEN** a bound group's binding command is confirmed in Telegram
- **THEN** the pane lists that group with its bound agent and an unlink control

#### Scenario: Admin connects the WhatsApp cloud lane
- **WHEN** an Owner selects the cloud lane, fills the labeled credential fields, and saves
- **THEN** the pane shows the gateway as connected, and presents the webhook callback URL and verify token to paste into the Meta dashboard

#### Scenario: Admin pairs a WhatsApp device
- **WHEN** an Owner selects the multi-device lane and starts pairing
- **THEN** the pane shows the ban-risk notice, a QR code with an 8-digit pair code alternative, and a live connection status that settles to connected after the scan

#### Scenario: Member pairs from the pane
- **WHEN** a signed-in member opens the pairing flow
- **THEN** a one-time `/start <token>` command is shown with a live expiry countdown, and after pairing completes the pane shows their linked platform identity with an unlink control

#### Scenario: Non-admin sees read-only
- **WHEN** a Member opens the Gateways pane
- **THEN** the admin controls (platform connection, lanes, default agent, bindings, enable/disable) are hidden or disabled, and only the personal pairing flow is offered
