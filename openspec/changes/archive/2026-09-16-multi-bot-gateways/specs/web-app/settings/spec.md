## MODIFIED Requirements

### Requirement: Gateways pane
The Gateways pane SHALL present gateway platforms through a second sidebar inside the pane: a section per platform (Telegram, WhatsApp) holding one status-bearing row per gateway account, with the selected account's configuration rendered in a detail pane to the right. Each row SHALL show the platform icon, the account's identity second line (@bot_username for Telegram; the linked lane identity for WhatsApp), and a status dot drawn from a shared vocabulary — Connected, Paused, Error, Not set up (WhatsApp linked state: Linked) — kept in sync with the detail pane's status card. Each platform section SHALL offer an add affordance ("Add a bot" / "Add an account") launching the connect wizard. At widths <768px the sidebar SHALL collapse into a horizontal scrollable chip row carrying the same dots, with the detail stacked beneath; the 360×800 minimum viewport SHALL NOT overflow horizontally.

The pane SHALL widen from the single `max-w-xl` column to the full settings content area (sidebar ~208px + flexing detail). Account selection SHALL be local component state; sections SHALL NOT add URL routes. The connect wizard SHALL require choosing the workspace agent the new account speaks for before it can be saved. The detail pane SHALL be per account: token (write-only, stored encrypted, shown back as a secret hint, replaceable), resolved bot username, bound agent (changeable via the agent picker), transport mode, enable/disable, status, and test message. All existing management surfaces persist: bound Telegram groups (bind by in-chat binding command addressed to the owning bot, list with the bound agent and owning bot, unlink). Member-facing, each platform's detail exposes the personal pairing flow: generate a one-time pairing token shown as a copyable command with an expiry countdown, and show/unlink the current platform-wide link for the signed-in member. Guard rejections (non-admin on admin actions, invalid tokens, one-binding-per-group conflicts) surface as toasts. The pane SHALL replace the mock Telegram entry in the legacy integrations list with the real surface.

#### Scenario: Sidebar lists one row per account
- **WHEN** an admin opens the Gateways pane with two Telegram bots and one linked WhatsApp account
- **THEN** the Telegram section lists two rows (one per bot, each with its dot and @username) and the WhatsApp section lists one Linked row

#### Scenario: Add affordance starts the wizard
- **WHEN** an admin selects "Add a bot" in the Telegram section
- **THEN** the connect wizard opens and requires both a bot token and a workspace agent before it can save

#### Scenario: Agent step is mandatory
- **WHEN** the admin attempts to complete the connect wizard without choosing an agent
- **THEN** the wizard refuses to save and names the agent field

#### Scenario: Per-account detail and status
- **WHEN** the admin selects one of the two Telegram rows
- **THEN** the detail pane shows that account's token hint, bound agent, transport, enable state, and status, while the other account keeps running

#### Scenario: Sidebar collapses on small viewports
- **WHEN** the viewport is narrower than 768px
- **THEN** the account rows render as a horizontal scrollable chip row above the stacked detail, carrying the same status dots, with no horizontal page overflow at 360px

#### Scenario: Status reflects a paused account
- **WHEN** a configured account is disabled
- **THEN** its sidebar row dot reads Paused and the detail status card agrees

#### Scenario: Admin connects a bot
- **WHEN** an Owner completes the wizard with a valid BotFather token and a chosen agent
- **THEN** the pane shows the new account row as connected with the bot username resolved from Telegram, and the token is never displayed again

#### Scenario: Default agent picker
- **WHEN** an admin opens the agent control on a gateway account's detail pane
- **THEN** the workspace's real agents are offered, and the saved choice pins that bot's direct messages to that agent

#### Scenario: Group binding listed
- **WHEN** a bound group's binding command is confirmed in Telegram
- **THEN** the pane lists that group with its bound agent and the owning bot, plus an unlink control

#### Scenario: Member pairs from the pane
- **WHEN** a signed-in member opens the pairing flow
- **THEN** a one-time pairing command is shown with a live expiry countdown, and after pairing completes the pane shows their linked identity with an unlink control covering all workspace bots

#### Scenario: Non-admin sees read-only
- **WHEN** a Member opens the Gateways pane
- **THEN** the admin controls (account management, agent binding, transport, bindings, enable/disable) are hidden or disabled, and only the personal pairing flow is offered
