# whatsapp-gateway Specification

## Purpose

Bridges WhatsApp into OnClaw agent sessions as a two-lane platform — the official Cloud API lane and an unofficial multi-device (whatsmeow) lane — covering lane-scoped configuration, member pairing, DM routing, digits-normalized session keys, per-lane streaming and approvals, attachment and voice ingress, delivery reliability under the 24-hour customer-service window, and webhook ingress with Meta signature validation. Like the Telegram gateway, it is a standalone ingress that never touches channels, the chokepoint, or work sessions.

## Requirements

### Requirement: Workspace WhatsApp gateway configuration with lanes
A workspace SHALL be able to configure one or more WhatsApp gateway accounts, each bound to exactly one workspace agent (required at save, identifying the number the account speaks for) and each on one of two lanes: `cloud_api` (official Meta Cloud API) or `multi_device` (an unofficial WhatsApp multi-device protocol client). The cloud lane SHALL store its credentials encrypted as one secret (access token, phone number id, app secret for webhook signature validation, and webhook verify token), entered as labeled fields and never returned in plaintext by any API; its transport SHALL be webhook-only. The multi-device lane SHALL hold no static credential: pairing SHALL happen through a QR code or an 8-digit pair code presented in the settings UI, with the resulting device session persisted server-side; the settings UI SHALL display a clear account-ban risk notice for this lane. Switching an account's lane SHALL preserve sessions, bindings, and paired identity links. Enabling and disabling SHALL be per account and SHALL stop that account's ingestion and outbound delivery without deleting its configuration, bindings, or sessions.

#### Scenario: Admin connects the cloud lane
- **WHEN** a workspace admin saves valid Cloud API credentials (access token, phone number id, app secret, verify token)
- **THEN** the account stores them as one encrypted secret, validates reachability against the Meta API, and begins webhook ingestion

#### Scenario: Admin pairs a device on the multi-device lane
- **WHEN** an admin selects the multi-device lane and scans the displayed QR code (or enters the displayed pair code) from a WhatsApp account
- **THEN** the gateway shows the device as connected, persists the device session, and begins message ingestion without any webhook URL

#### Scenario: Lane switch preserves conversations
- **WHEN** an admin switches a WhatsApp account from one lane to the other
- **THEN** existing DM sessions, the account's bound-agent routing, and paired member links continue to route and resume without re-pairing

#### Scenario: Disabled account is inert
- **WHEN** an admin disables one connected WhatsApp account and a member then messages that number
- **THEN** no run is minted, no reply is delivered, the stored configuration and bindings are preserved, and the workspace's other accounts continue serving

#### Scenario: Credentials never leak
- **WHEN** any gateway API returns WhatsApp configuration to a client
- **THEN** the cloud-lane secret is presented as a write-only secret hint, and no multi-device session key material is exposed

### Requirement: Pairing and member identity
Access SHALL be default-deny: only paired WhatsApp identities may trigger turns. A workspace member SHALL generate a one-time pairing token that expires within a bounded window; the member sends `/start <token>` to any one of the workspace's WhatsApp accounts in a direct message, and the gateway SHALL bind the WhatsApp identity — keyed on the immutable wa id (phone number digits), never the display name — to that member and workspace. Pairing SHALL be revocable and SHALL be platform-level: one link covers the member's identity with every gateway account in the workspace. Every minted run SHALL carry the paired member's user id so hooks, permissions, memory, and tool gates evaluate under that human's RBAC. Unpaired senders SHALL receive a pairing hint and SHALL NOT reach the runner.

#### Scenario: Successful pairing
- **WHEN** a member sends `/start <valid-unexpired-token>` to any one of the workspace's WhatsApp accounts from a WhatsApp account
- **THEN** the gateway stores a link between that wa id and the member, confirms in-chat, and consumes the token (a second use fails)

#### Scenario: One link spans all accounts
- **WHEN** a paired member sends a direct message to a different workspace WhatsApp account they have never contacted before
- **THEN** the message is accepted as a paired member without a new pairing flow, and the turn runs under the same member identity

#### Scenario: Unlink is platform-wide
- **WHEN** a member (or an admin) unlinks a WhatsApp identity
- **THEN** the identity is unpaired from every gateway account in the workspace

#### Scenario: Unpaired sender refused
- **WHEN** an unpaired WhatsApp user sends any message to a workspace WhatsApp account
- **THEN** no run is minted and the sender receives a pairing hint

#### Scenario: Runs execute under the member's identity
- **WHEN** a paired member triggers a turn from WhatsApp
- **THEN** the run carries that member's user id and workspace scope, and permission-gated behavior matches an equivalent web turn by the same member

### Requirement: Direct-message routing to agents only
A direct message to a WhatsApp account SHALL route to that account's bound agent as a private session — the number a member messages IS the agent selection, and per-user agent overrides SHALL NOT apply to gateway DMs. In v1 the WhatsApp gateway SHALL accept direct messages only: group chats SHALL NOT be bound, ingested, or answered on either lane. The gateway SHALL NOT route any WhatsApp surface to channels, work sessions, or the channel chokepoint.

#### Scenario: DM routes to the bound agent
- **WHEN** a paired member sends a direct message to the WhatsApp account bound to agent Beacon
- **THEN** the turn runs on Beacon under that member's identity — the number selects the agent, and per-user agent overrides do not apply to gateway DMs

#### Scenario: Two accounts yield two sessions
- **WHEN** a paired member direct-messages the WhatsApp account bound to Atlas and later the account bound to Cargo
- **THEN** the member holds two separate private sessions, one per agent, each resumable

#### Scenario: Group messages are not ingested
- **WHEN** a message arrives in a WhatsApp group the paired number participates in
- **THEN** no run is minted on either lane and no reply is sent to the group

#### Scenario: No channel artifacts
- **WHEN** any WhatsApp turn is minted
- **THEN** the ExecRequest carries no channel id, root message id, chain depth, or work session id, and no channel feed, summon decision, or fan-out event is produced

### Requirement: WhatsApp session keys and lifecycle
Gateway sessions SHALL use deterministic keys `wa_dm_<wa_id>_<agent_id>` with a numeric suffix bump (`..._<n>`) for `/new`, persisted across restarts so conversations resume with full transcript. The wa id SHALL be normalized to bare phone digits on both lanes (the cloud lane's wa id and the multi-device lane's JID localpart), which is what makes a lane switch session-preserving. `/new` SHALL archive the current session and mint a fresh one, preserving prior transcripts; `/compact` SHALL run the existing compaction-as-a-turn; `/usage` SHALL reply with the session's context-meter numbers. Commands SHALL be accepted as plain leading-slash text in direct messages.

#### Scenario: Session resumes after restart
- **WHEN** a paired member sends a new message after a gateway/server restart
- **THEN** the turn lands on the same deterministic session key and the reply reflects the prior transcript context

#### Scenario: New command resets conversation
- **WHEN** a member sends `/new` in a DM whose current session is `wa_dm_628123456789_atlas` with prior suffixes 1 and 2
- **THEN** the next message runs on `wa_dm_628123456789_atlas_3` and sessions 0–2 remain readable

#### Scenario: Session key is identical across lanes
- **WHEN** the same WhatsApp user converses before and after an account lane switch
- **THEN** both lanes derive the identical `wa_dm_<digits>_<agent_id>` key from their respective identity formats

### Requirement: Streaming delivery per lane capabilities
Outbound turn delivery SHALL adapt to what each lane supports via adapter capability negotiation. The cloud lane — which cannot edit sent messages — SHALL show the typing indicator (driven by marking the user's message read with the typing flag) on a heartbeat cadence while the turn runs, deliver no placeholder message, and deliver the final reply as a single message through the durable outbox. The multi-device lane — which can edit — SHALL stream with the placeholder-plus-debounced-edit behavior used by the Telegram gateway, plus its own typing presence. On both lanes, LLM markdown SHALL be converted to the WhatsApp formatting subset (bold, italic, strikethrough, monospace, code blocks) with unsupported constructs degraded (lists rendered as bullet-prefixed lines, headings as bold text), and a response exceeding the platform message limit SHALL be split at natural boundaries with code fences cleanly closed and reopened. The cloud lane's read-receipt side effect (the user's message showing as read once typing starts) SHALL be accepted behavior.

#### Scenario: Cloud lane delivers one final message
- **WHEN** an agent turn streams a long reply over the cloud lane
- **THEN** the user sees the typing indicator while the turn runs, receives exactly one final reply message, and no placeholder or intermediate message is sent

#### Scenario: Multi-device lane streams with edits
- **WHEN** an agent turn streams a long reply over the multi-device lane
- **THEN** a placeholder message is updated on a debounce cadence and the final edit carries the complete reply

#### Scenario: Split preserves code fences
- **WHEN** a reply exceeds the WhatsApp message limit in the middle of a fenced code block
- **THEN** the earlier part ends with the fence closed and the next part reopens the fence with the same language tag

#### Scenario: Unsupported markdown degrades
- **WHEN** a reply contains unordered lists and headings
- **THEN** the delivered WhatsApp text renders list items as bullet-prefixed lines and headings as bold text, never as raw markup artifacts

### Requirement: Approval bridge per lane
When a turn raises the shell-command approval interrupt, the gateway SHALL present the decision in the chat that raised the turn, restricted to paired members, and while an approval is pending, new turns on that session SHALL be refused with a pending-approval notice. On the cloud lane the card SHALL be an interactive message with Approve and Deny quick-reply buttons whose button reply ids carry the existing platform-neutral approval callback encoding; a button tap SHALL resume the interrupted turn through the existing approval-resume path, and because sent messages cannot be edited, the resolution SHALL be recorded by a follow-up receipt message rather than by editing the card. On the multi-device lane — where native buttons are not usable — the card SHALL be a plain message instructing an `APPROVE` / `DENY` text reply; while the card is pending, the next direct-message text SHALL be intercepted, matched case-insensitively as the decision, and any other text SHALL receive the pending-approval notice.

#### Scenario: Cloud button approve resumes the turn
- **WHEN** a permitted member taps the Approve quick reply on the cloud lane
- **THEN** the interrupted tool call re-executes through the resume path and a receipt message records the approval and actor

#### Scenario: Cloud deny blocks without failing the run
- **WHEN** a member taps Deny on the cloud lane
- **THEN** the canonical block result is returned as the tool outcome, the turn continues to completion, and the run does not fail

#### Scenario: Multi-device text approval resumes the turn
- **WHEN** a pending approval card exists on the multi-device lane and the paired member replies `approve` in the DM
- **THEN** the reply is treated as the decision, the turn resumes through the approval path, and a receipt message records the outcome

#### Scenario: Multi-device non-decision text is refused
- **WHEN** a pending approval card exists and the member sends text that is not APPROVE or DENY
- **THEN** the gateway replies that an approval is pending and does not mint a new turn

#### Scenario: Unpaired identity cannot approve
- **WHEN** an unpaired WhatsApp identity taps a cloud approval button
- **THEN** the decision is refused and no turn is resumed

### Requirement: Attachment and voice ingress
Images, documents, audio, and video sent to a workspace WhatsApp account SHALL be downloaded and normalized into the existing attachment pipeline with the appropriate lane (inline image, inline PDF, inline text, or drop) exactly as web uploads are, subject to the same size caps; video SHALL be treated as a document-lane attachment; stickers SHALL be refused with an explanatory notice. Voice notes SHALL be transcribed to text before the turn with the transcript prefixed as a voice note in the turn input; when no speech-to-text provider is configured, voice notes SHALL be refused with a fail-soft notice rather than failing the gateway. Oversized files SHALL be refused with an explanatory message.

#### Scenario: Photo becomes an inline image
- **WHEN** a paired member sends a photo within the size cap with a caption question
- **THEN** the turn carries the image as an inline-lane attachment plus the caption text, and the agent can see the image

#### Scenario: Video lands as a document
- **WHEN** a paired member sends a video within the size cap
- **THEN** the turn carries it as a document-lane attachment the agent can read through the document tools

#### Scenario: Sticker refused
- **WHEN** a paired member sends a sticker
- **THEN** the bot replies that stickers are not supported and mints no run

#### Scenario: Voice note transcribed
- **WHEN** a paired member sends a voice note and a speech-to-text provider is configured
- **THEN** the turn input is the transcription prefixed as a voice note

#### Scenario: Voice without STT fails soft
- **WHEN** a voice note arrives with no speech-to-text provider configured
- **THEN** the bot replies that voice is unavailable and mints no run

### Requirement: Delivery outbox and the 24-hour window
Every outbound WhatsApp message SHALL be recorded in the durable outbox before or as it is sent, with the shared at-least-once semantics: crash redelivery on startup, bounded attempts within a freshness window, duplicate-warning prefixes for ambiguous mid-crash sends, and retention pruning of delivered rows. On the cloud lane, a send rejected because the 24-hour customer-service window has expired SHALL mark the outbox entry dead (never retried) and SHALL surface observably in gateway health and logs; the gateway SHALL NOT attempt template-based re-engagement. The multi-device lane has no messaging window and SHALL NOT dead-letter for window reasons.

#### Scenario: Crash redelivery
- **WHEN** the server crashes after the agent completes a reply but before the WhatsApp send succeeds, then restarts
- **THEN** the reply is delivered on startup

#### Scenario: Window-expired delivery dies observably
- **WHEN** a cloud-lane reply is rejected with the window-expired error after the customer-service window closed
- **THEN** the outbox entry is marked dead without further attempts and the failure is visible in gateway health and logs

### Requirement: Webhook ingress with Meta validation
The cloud lane's webhook ingress SHALL be served at a fixed public path where every POST is validated against the `X-Hub-Signature-256` HMAC computed with the configured app secret before parsing; invalid or missing signatures SHALL be rejected unauthenticated. The verification handshake (`GET` with the verify token) SHALL be answered with the echoed challenge value when the token matches. Inbound payloads carry batched entries: message events SHALL be parsed and redelivered duplicates dropped by a message-id dedup ring; message-status events (sent, delivered, read) and other event kinds SHALL be ignored without error. Webhook registration with Meta remains a manual operator step; the settings UI SHALL present the exact callback URL and verify token to paste.

#### Scenario: Bad signature rejected
- **WHEN** a POST arrives at the WhatsApp webhook path with a missing or non-matching signature header
- **THEN** it is rejected unauthenticated and not processed

#### Scenario: Verification challenge echoed
- **WHEN** Meta sends the webhook verification GET with the correct verify token
- **THEN** the ingress responds with the challenge value so the webhook can be registered

#### Scenario: Duplicate webhook delivery dropped
- **WHEN** Meta redelivers a webhook batch containing an already-processed message id
- **THEN** the duplicate message produces no run and no reply

#### Scenario: Status-only payload ignored
- **WHEN** a webhook batch contains only message-status events
- **THEN** it is acknowledged without error and no runs are minted

### Requirement: Gateway guardrails
The gateway SHALL protect itself and the workspace token budget: a per-chat loop guard SHALL drop runaway bot-to-bot message loops after a bounded rate with a cooldown, and a circuit breaker SHALL auto-pause a gateway lane after repeated consecutive send or ingestion failures and SHALL resume only on explicit admin action. The multi-device lane SHALL additionally manage its device connection lifecycle: automatic reconnection with backoff when the connection drops, and a surfaced disconnected state when the device goes offline.

#### Scenario: Bot loop broken
- **WHEN** runaway bot-to-bot traffic hits a bound chat
- **THEN** after the configured threshold the gateway drops incoming bot messages for a cooldown window instead of minting runs

#### Scenario: Circuit breaker pauses a failing lane
- **WHEN** a lane's platform calls fail repeatedly in succession
- **THEN** ingestion is paused, admins are notified, and no runs are minted until an admin resumes the gateway

#### Scenario: Device reconnects after a drop
- **WHEN** the multi-device connection drops and recovers
- **THEN** the lane reconnects with backoff and resumes ingestion without re-pairing

### Requirement: Gateway admin API and surfaces
The server SHALL expose workspace-scoped REST endpoints (admin-permission-gated) for WhatsApp gateway configuration: save per-lane configuration, enable/disable per account, probe lane health, and present the multi-device pairing flow (start pairing, poll for QR or pair code, connection status, logout device). Pairing-token generation, revocation, and per-member unpairing SHALL follow the existing gateway pairing API shape. No group-binding endpoints SHALL exist for WhatsApp in v1.

#### Scenario: Non-admin refused
- **WHEN** a Member calls any WhatsApp gateway admin endpoint
- **THEN** the request is rejected with a permission error

#### Scenario: Lane-scoped validation
- **WHEN** an admin saves cloud-lane configuration with a missing field, or selects long-polling transport for the cloud lane
- **THEN** the request is rejected with a field-level error naming the problem

### Requirement: Gateway DM sessions in the private session index
A WhatsApp direct-message session SHALL register in the paired member's private per-user session index with a WhatsApp origin indicator, following the existing durable-index contract (birth title from input, activity bumps, soft delete).

#### Scenario: DM session listed for its owner
- **WHEN** a paired member lists their sessions for the agent they talk to over WhatsApp
- **THEN** the WhatsApp DM session appears among their sessions with a WhatsApp origin indicator
