# telegram-gateway Specification

## Purpose

Bridges Telegram into OnClaw agent sessions: workspace-scoped bot configuration, member pairing, chat-to-agent bindings, session-key management, streaming message rendering, approval resumption, attachment and voice ingress, and delivery reliability — as a standalone ingress that never touches channels, the chokepoint, or work sessions.

## Requirements

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

### Requirement: Telegram session keys and lifecycle
Gateway sessions SHALL use deterministic keys: `tg_dm_<telegram_user_id>_<agent_id>` for direct messages and `tg_group_<chat_id>_<agent_id>` for groups, persisted across restarts so conversations resume with full transcript. A `/new` command SHALL archive the current session and mint a fresh one by numeric suffix bump (`..._<n>`), preserving prior transcripts. `/compact` SHALL run the existing compaction-as-a-turn, and `/usage` SHALL reply with the session's context-meter numbers. All three commands SHALL be accepted in DMs; `/compact` and `/new` SHALL be accepted in bound groups from paired members.

#### Scenario: Session resumes after restart
- **WHEN** a paired member sends a new message after a gateway/server restart
- **THEN** the turn lands on the same deterministic session key and the reply reflects the prior transcript context

#### Scenario: New command resets conversation
- **WHEN** a member sends `/new` in a DM whose current session is `tg_dm_593821092_atlas` with prior suffixes 1 and 2
- **THEN** the next message runs on `tg_dm_593821092_atlas_3` and sessions 0–2 remain readable

#### Scenario: Compact runs as a turn
- **WHEN** a member sends `/compact` in a bound group session
- **THEN** the existing compaction turn executes on that session and a divider is recorded, exactly as in web chat

### Requirement: Turn attribution in shared sessions
In a group session, each turn SHALL be attributed to its invoker: the turn input SHALL be prefixed with the sender's display name and handle so the agent knows who is speaking, and the run SHALL execute under that sender's member identity. Consecutive turns in one group session MAY therefore run under different members' RBAC.

#### Scenario: Multi-speaker group
- **WHEN** Oni (Admin) and Sari (Member) each mention the bot in the same bound group
- **THEN** Oni's turn runs under Oni's identity with input prefixed `[Oni (@onih)]:`, and Sari's under Sari's identity with her own prefix

### Requirement: Streaming rendering
Outbound turn delivery SHALL stream: an initial placeholder message is edited on a debounce cadence within Telegram's per-chat edit rate limits, with a typing indicator heartbeat while the model thinks or tools run. LLM markdown SHALL be converted to Telegram-safe HTML with automatic balancing of tags opened mid-stream, and a response exceeding the platform message limit SHALL be split at natural boundaries with code fences cleanly closed on one part and reopened on the next. If Telegram rejects formatted text, delivery SHALL retry once as plain text rather than dropping the message.

#### Scenario: Debounced live edits
- **WHEN** an agent streams a long reply to a Telegram chat
- **THEN** the placeholder message is updated at most once per debounce interval, and the final edit carries the complete reply

#### Scenario: Split preserves code fences
- **WHEN** a reply exceeds the Telegram message limit in the middle of a fenced code block
- **THEN** the earlier part ends with the fence closed and the next part reopens the fence with the same language tag

#### Scenario: Parse failure falls back to plain text
- **WHEN** Telegram rejects an edited or sent message as unparseable
- **THEN** the gateway retries the same content stripped of markup and the message is delivered

### Requirement: Approval bridge over inline keyboards
When a turn raises the shell-command approval interrupt, the gateway SHALL render an approve/deny inline keyboard in the chat that raised the turn. Tapping a button SHALL resume the interrupted turn through the existing approval-resume path with that decision, SHALL update the card to record the decision and actor, and SHALL be restricted to paired members permitted to approve. While an approval is pending, new turns on that session SHALL be refused with a pending-approval notice; the buttons remain the only path forward.

#### Scenario: Approve resumes the turn
- **WHEN** a permitted member taps Approve on the approval card
- **THEN** the interrupted tool call re-executes through the resume path and the card updates to show the approval and actor

#### Scenario: Deny blocks without failing the run
- **WHEN** a member taps Deny
- **THEN** the canonical block result is returned as the tool outcome, the turn continues to completion, and the run does not fail

#### Scenario: Pending approval blocks new turns
- **WHEN** another message arrives in the chat while an approval card is unanswered
- **THEN** the gateway replies that an approval is pending and does not mint a new turn

### Requirement: Attachment and voice ingress
Photos and documents sent to the bot SHALL be downloaded and normalized into the existing attachment pipeline with the appropriate lane (inline image, inline PDF, inline text, or drop) exactly as web uploads are, subject to the same size caps; oversized files SHALL be refused with an explanatory message. Voice notes SHALL be transcribed to text before the turn; the transcript SHALL be prefixed as a voice note in the turn input. When no speech-to-text provider is configured, voice notes SHALL be refused with a fail-soft notice rather than failing the gateway.

#### Scenario: Photo becomes an inline image
- **WHEN** a paired member sends a photo within the size cap with a caption question
- **THEN** the turn carries the image as an inline-lane attachment plus the caption text, and the agent can see the image

#### Scenario: Voice note transcribed
- **WHEN** a paired member sends a voice note and a speech-to-text provider is configured
- **THEN** the turn input is the transcription prefixed as a voice note

#### Scenario: Voice without STT fails soft
- **WHEN** a voice note arrives with no speech-to-text provider configured
- **THEN** the bot replies that voice is unavailable and mints no run

### Requirement: Delivery outbox with at-least-once semantics
Every outbound gateway message SHALL be recorded in a durable outbox before or as it is sent. If the process crashes between generating a reply and delivering it, the reply SHALL be redelivered on next startup. Redelivery SHALL be bounded (a small attempt count within a freshness window), duplicates of ambiguous mid-crash sends SHALL carry a duplicate-warning prefix, and delivered outbox rows SHALL be pruned after a retention period.

#### Scenario: Crash redelivery
- **WHEN** the server crashes after the agent completes a reply but before the Telegram send succeeds, then restarts
- **THEN** the reply is delivered to the chat on startup

#### Scenario: Bounds and pruning
- **WHEN** an outbox entry exceeds its attempt budget or freshness window, or a delivered entry ages past retention
- **THEN** it is marked dead (no further attempts) or pruned respectively, without blocking other deliveries

### Requirement: Busy queueing
When a message arrives for a session whose agent run is still active, the gateway SHALL NOT fail: the message SHALL be queued and submitted as the next turn's input once the active run completes, with a brief acknowledgment sent to the chat. Queue depth SHALL be bounded per session.

#### Scenario: Message during an active run
- **WHEN** a second message arrives in a bound group while the group agent's run is streaming
- **THEN** the sender receives a queued acknowledgment and the message becomes the next turn after the active run completes

### Requirement: Gateway guardrails
The gateway SHALL protect itself and the workspace token budget: a per-chat bot-loop guard SHALL drop runaway bot-to-bot message loops after a bounded rate with a cooldown; a circuit breaker SHALL auto-pause ingestion for a gateway after repeated consecutive failures and SHALL resume only on explicit admin action; and when Telegram reports a group chat id migration (`migrate_to_chat_id`), existing bindings and session keys SHALL be transparently remapped to the new chat id so routing and history survive the migration.

#### Scenario: Bot loop broken
- **WHEN** two bots in a bound group trigger each other repeatedly
- **THEN** after the configured per-chat threshold the gateway drops incoming bot messages for a cooldown window instead of minting runs

#### Scenario: Circuit breaker pauses a failing gateway
- **WHEN** a gateway's Telegram API calls fail repeatedly in succession
- **THEN** ingestion is paused, admins are notified, and no runs are minted until an admin resumes the gateway

#### Scenario: Chat id migration survives
- **WHEN** Telegram reports that a bound group has migrated to a new chat id
- **THEN** the binding is remapped to the new id and subsequent messages in the same group continue on the migrated session without admin intervention

### Requirement: Gateway admin API and surfaces
The server SHALL expose workspace-scoped REST endpoints (admin-permission-gated) for gateway configuration (create/update/enable/disable/test), group binding management (create/list/delete), pairing-token generation and revocation, and per-member unpairing. The Telegram webhook ingress SHALL be served at a fixed path with secret-token validation; long-polling SHALL run in-process and require no public URL.

#### Scenario: Non-admin refused
- **WHEN** a Member calls any gateway admin endpoint
- **THEN** the request is rejected with a permission error

#### Scenario: Webhook secret enforced
- **WHEN** a POST arrives at the webhook path without the configured secret token
- **THEN** it is rejected unauthenticated and not processed

### Requirement: Gateway DM sessions in the private session index
A gateway direct-message session SHALL register in the paired member's private per-user session index with a Telegram origin indicator, following the existing durable-index contract (birth title from input, activity bumps, soft delete). Gateway group sessions SHALL NOT appear in any per-user session index; they remain accessible by direct session id and through their transcript events.

#### Scenario: DM session listed for its owner
- **WHEN** a paired member lists their sessions for the agent they talk to over Telegram
- **THEN** the Telegram DM session appears among their sessions with a Telegram origin indicator

#### Scenario: Group session not listed
- **WHEN** any member lists sessions for the bound agent
- **THEN** the gateway group session does not appear in the per-user listing

### Requirement: Per-account webhook ingress
Each gateway account SHALL expose its own public webhook ingress path containing the account's stable identifier (not the workspace slug), authenticated by a per-account secret header. Telegram SHALL require one distinct webhook URL per bot token; two accounts SHALL never share an ingress path or secret. A stopped or unknown account SHALL answer the ingress with not-found and process nothing.

#### Scenario: Distinct ingress per account
- **WHEN** two Telegram accounts on one workspace both use webhook transport
- **THEN** each is registered with Telegram under its own webhook URL carrying its own account identifier and secret, and deliveries arrive segregated per account

#### Scenario: Unknown account at ingress
- **WHEN** a webhook delivery arrives for an account id that does not exist or is not running
- **THEN** the ingress answers not-found without processing the payload
