## MODIFIED Requirements

### Requirement: Workspace gateway configuration
A workspace SHALL be able to configure one or more Telegram gateway accounts: each account is an encrypted bot token, a bot username identity, an enabled flag, exactly one bound agent, and a transport mode (`webhook` with a public URL, or `long-polling`). Multiple accounts per workspace SHALL be supported; each account's bound agent SHALL be required at save and identifies the agent the bot speaks for. The workspace-level default-agent setting SHALL NOT exist: an account's bound agent is the only DM routing configuration. Gateway credentials SHALL be stored encrypted and never returned in plaintext by any API. Enabling and disabling SHALL be per account and SHALL stop that account's ingestion and outbound delivery without deleting its configuration, bindings, or sessions.

A configuration update SHALL be validated as one atomic unit: activating webhook transport SHALL require both the transport value and an `https://` webhook URL to be effective in the same save, and a save SHALL NOT report success for a value the validator discards. A request that omits the webhook URL SHALL carry the stored webhook URL forward unchanged. A transport or URL change submitted while no bot token is stored on the account SHALL be refused with a fielded validation error naming `token`.

#### Scenario: Admin connects a bot
- **WHEN** a workspace admin saves a valid bot token obtained from BotFather together with a workspace agent
- **THEN** a gateway account is stored with the token encrypted, the bot username resolved from the Telegram API, and the bound agent recorded

#### Scenario: Second bot on the same platform
- **WHEN** an admin adds a second Telegram bot token bound to a different agent
- **THEN** both accounts run concurrently, each with its own username, transport, status, and enable state

#### Scenario: Agent binding is required
- **WHEN** an admin attempts to save a gateway account without naming a workspace agent
- **THEN** the save is refused with a fielded validation error naming the agent field

#### Scenario: Disabled gateway is inert
- **WHEN** an admin disables one of two connected accounts and a member sends a Telegram message to that bot
- **THEN** no run is minted for that account, no reply is delivered, and the other account continues serving

#### Scenario: Token never leaks
- **WHEN** any gateway API returns account configuration to a client
- **THEN** each bot token is presented as a write-only secret hint, never its plaintext value

#### Scenario: Webhook activation is atomic
- **WHEN** a client activates webhook transport without providing an `https://` webhook URL and none is stored
- **THEN** the configuration is refused with a fielded validation error naming `webhook_url`, the transport remains unchanged, and no partially-valid save persists

#### Scenario: Omitted webhook URL carries forward
- **WHEN** a client submits a configuration update that changes transport but omits `webhook_url` while an `https://` URL is already stored
- **THEN** the stored webhook URL is kept and the save succeeds

#### Scenario: Transport change requires a connected gateway
- **WHEN** a client submits a transport or webhook URL change while no bot token is stored on the account
- **THEN** the request is refused with a fielded validation error naming `token` and the stored transport is unchanged

#### Scenario: Long-polling switch reports its clearing
- **WHEN** a client switches transport to long-polling while a webhook URL is stored
- **THEN** the save succeeds, ingestion switches to long-polling, and the stored webhook URL is cleared (the response reflects no webhook URL)

### Requirement: Pairing and member identity
Access SHALL be default-deny: only paired Telegram identities may trigger turns. A workspace member SHALL generate a one-time pairing token (via the settings UI or API) that expires within a bounded window; the member sends it to any one of the workspace's bots in Telegram (`/start <token>` for DMs), and the gateway SHALL bind the Telegram user id — keyed on the immutable numeric id, never the username — to that member and workspace. Pairing SHALL be revocable and SHALL be platform-level: one link covers the member's identity with every gateway account in the workspace. Every minted run SHALL carry the paired member's user id so hooks, permissions, memory, and tool gates evaluate under that human's RBAC. Unpaired senders SHALL receive a pairing hint and SHALL NOT reach the runner.

#### Scenario: Successful pairing
- **WHEN** a member sends `/start <valid-unexpired-token>` to any one of the workspace's bots from a Telegram account
- **THEN** the gateway stores a link between that Telegram user id and the member, confirms in-chat, and consumes the token (a second use fails)

#### Scenario: One link spans all bots
- **WHEN** a paired member sends a direct message to a different workspace bot they have never contacted before
- **THEN** the message is accepted as a paired member without a new pairing flow, and the turn runs under the same member identity

#### Scenario: Unlink is platform-wide
- **WHEN** a member (or an admin) unlinks a Telegram identity
- **THEN** the identity is unpaired from every gateway account in the workspace

#### Scenario: Expired token refused
- **WHEN** a user sends a pairing command whose token has expired
- **THEN** the bot replies that the link is invalid or expired and no identity link is created

#### Scenario: Unpaired sender refused
- **WHEN** an unpaired Telegram user sends any message to a workspace bot in a DM or a bound group
- **THEN** no run is minted and the sender receives a pairing hint

#### Scenario: Runs execute under the member's identity
- **WHEN** a paired member triggers a turn from Telegram
- **THEN** the run carries that member's user id and workspace scope, and permission-gated behavior (hooks, tool gates, memory writes) matches an equivalent web turn by the same member

### Requirement: Chat bindings route to agents only
A direct message to a gateway account SHALL route to that account's bound agent as a private session — the bot a member messages IS the agent selection, and per-user agent overrides SHALL NOT apply to gateway DMs. A group chat SHALL route to the one agent of the one account bound to that group; each group chat SHALL have at most one binding across all workspace accounts, and the binding SHALL name its owning account. Bindings SHALL be created by an admin (via the settings UI) and confirmed in-chat with a binding command addressed to the owning bot. Adding a second workspace bot to an already-bound group SHALL surface a configuration warning, and the second bot SHALL NOT answer in that group. The gateway SHALL NOT route any Telegram surface to channels, work sessions, or the channel chokepoint, and SHALL NOT ingest group chatter that does not target a workspace bot (Telegram privacy mode is expected ON; only commands, bot replies, and bot mentions reach ingestion).

#### Scenario: DM uses member default agent
- **WHEN** a paired member sends a direct message to the gateway account bound to agent Beacon
- **THEN** the turn runs on Beacon under that member's identity — the bot selects the agent, and per-user agent overrides do not apply to gateway DMs

#### Scenario: DM falls back to gateway default
- **WHEN** a paired member with no per-user agent choice sends a direct message to the gateway account bound to Cargo
- **THEN** the turn runs on Cargo — the account's bound agent is the only DM routing configuration, and no workspace-level default exists

#### Scenario: Two bots yield two sessions
- **WHEN** a paired member direct-messages the bot bound to Atlas and later the bot bound to Cargo
- **THEN** the member holds two separate private sessions, one per agent, each resumable

#### Scenario: One agent per group
- **WHEN** an admin binds Telegram group G to account A (agent Atlas) and later attempts to bind G to account B
- **THEN** the second binding is rejected and G continues to route to Atlas via account A

#### Scenario: One bot per group
- **WHEN** a second workspace bot joins a group that is already bound to account A and is @mentioned there
- **THEN** the configuration warns about the unbound bot, the second bot does not answer, and G continues to route to Atlas via account A

#### Scenario: Group mention triggers a turn
- **WHEN** a paired member @mentions the bound bot or replies to one of its messages in a bound group
- **THEN** the turn runs on that account's bound agent as a shared session turn attributed to that member

#### Scenario: No channel artifacts
- **WHEN** any Telegram turn is minted
- **THEN** the ExecRequest carries no channel id, root message id, chain depth, or work session id, and no channel feed, summon decision, or fan-out event is produced

## ADDED Requirements

### Requirement: Per-account webhook ingress
Each gateway account SHALL expose its own public webhook ingress path containing the account's stable identifier (not the workspace slug), authenticated by a per-account secret header. Telegram SHALL require one distinct webhook URL per bot token; two accounts SHALL never share an ingress path or secret. A stopped or unknown account SHALL answer the ingress with not-found and process nothing.

#### Scenario: Distinct ingress per account
- **WHEN** two Telegram accounts on one workspace both use webhook transport
- **THEN** each is registered with Telegram under its own webhook URL carrying its own account identifier and secret, and deliveries arrive segregated per account

#### Scenario: Unknown account at ingress
- **WHEN** a webhook delivery arrives for an account id that does not exist or is not running
- **THEN** the ingress answers not-found without processing the payload
