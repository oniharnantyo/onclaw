## MODIFIED Requirements

### Requirement: Workspace gateway configuration
A workspace SHALL be able to configure exactly one Telegram gateway: an encrypted bot token, a bot username, an enabled flag, a default agent for direct messages, and a transport mode (`webhook` with a public URL, or `long-polling`). Gateway credentials SHALL be stored encrypted and never returned in plaintext by any API. Disabling a gateway SHALL stop ingestion and outbound delivery without deleting configuration, bindings, or sessions.

A configuration update SHALL be validated as one atomic unit: activating webhook transport SHALL require both the transport value and an `https://` webhook URL to be effective in the same save, and a save SHALL NOT report success for a value the validator discards. A request that omits the webhook URL SHALL carry the stored webhook URL forward unchanged. A transport or URL change submitted while no bot token is stored SHALL be refused with a fielded validation error naming `token`.

#### Scenario: Admin connects a bot
- **WHEN** a workspace admin saves a valid bot token obtained from BotFather
- **THEN** the gateway stores the token encrypted, resolves the bot username from the Telegram API, and starts ingestion in the configured transport mode

#### Scenario: Disabled gateway is inert
- **WHEN** an admin disables a connected gateway and a member sends a Telegram message to the bot
- **THEN** no run is minted, no reply is delivered, and the stored configuration and bindings are preserved

#### Scenario: Token never leaks
- **WHEN** any gateway API returns configuration to a client
- **THEN** the bot token is presented as a write-only secret hint, never its plaintext value

#### Scenario: Webhook activation is atomic
- **WHEN** a client activates webhook transport without providing an `https://` webhook URL and none is stored
- **THEN** the configuration is refused with a fielded validation error naming `webhook_url`, the transport remains unchanged, and no partially-valid save persists

#### Scenario: Omitted webhook URL carries forward
- **WHEN** a client submits a configuration update that changes transport but omits `webhook_url` while an `https://` URL is already stored
- **THEN** the stored webhook URL is kept and the save succeeds

#### Scenario: Transport change requires a connected gateway
- **WHEN** a client submits a transport or webhook URL change while no bot token is stored for the workspace
- **THEN** the request is refused with a fielded validation error naming `token` and the stored transport is unchanged

#### Scenario: Long-polling switch reports its clearing
- **WHEN** a client switches transport to long-polling while a webhook URL is stored
- **THEN** the save succeeds, ingestion switches to long-polling, and the stored webhook URL is cleared (the response reflects no webhook URL)
