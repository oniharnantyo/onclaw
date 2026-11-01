# channels Specification (delta)

## Purpose

Let an authorized user converse with an agent through Telegram — sending messages away from the desk and receiving streamed replies, approvals, artifacts, and schedule results — while the gateway runs in the tray. The channel is the second trigger surface after the scheduler and the first outbound delivery target.

## ADDED Requirements

### Requirement: Channel lifecycle

An enabled Telegram channel SHALL poll for incoming messages while the gateway is running, hold polling while the gateway is paused, and stop when the gateway stops or the app closes. A disabled channel MUST NOT make any Telegram API request. Channel state changes (enable, disable, token change) SHALL take effect without an app restart, and the channel MUST NOT block app startup or shutdown — teardown is bounded even when the network is unresponsive.

#### Scenario: Enabled channel follows the gateway

- **WHEN** the gateway transitions from paused to running with an enabled, configured channel
- **THEN** the channel begins polling and an inbound message dispatches a turn

#### Scenario: Disabled channel is inert

- **WHEN** the channel is disabled
- **THEN** no message to the bot dispatches a turn and no polling occurs

### Requirement: Bot credential secrecy

The bot token MUST be returned to no frontend read or listing. Reads SHALL expose only whether a token is set and at most a short non-secret hint derived from it (the last 4 characters). Updating the channel with a blank token SHALL keep the stored token; only a non-blank input replaces it.

#### Scenario: Token never echoes back

- **WHEN** the Channels pane reopens after saving a token
- **THEN** the token field is blank and the stored state shows only set/not-set with the short hint

### Requirement: Default-deny authorization

Only senders on the channel's user allowlist MAY trigger turns. A DM from an unlisted user MUST NOT dispatch a turn; the bot SHALL respond only with the pairing affordance (never a policy explanation that leaks state). Group messages are additionally gated by the group rules. Authorization MUST fail closed: an absent or unreadable allowlist denies everyone.

#### Scenario: Unknown DM sender cannot trigger

- **WHEN** a user not on the allowlist sends a DM
- **THEN** no turn is dispatched and the bot offers the pairing path

#### Scenario: Allowlisted user triggers

- **WHEN** an allowlisted user sends a DM text message
- **THEN** the message dispatches a turn on the chat's session

### Requirement: Pairing flow

A `/start` (or first) message from an unlisted DM sender SHALL make the bot reply with that sender's numeric Telegram user ID and hold the request as a pending pairing. The pending pairing SHALL appear in the UI (Channels pane) as an approval card with allow and dismiss actions. Approving adds the sender's ID to the allowlist and the bot confirms; dismissing clears it. Pending pairings SHALL be bounded (a small cap per sender) so the flow cannot be flooded.

#### Scenario: Pairing completes

- **WHEN** the user approves a pending pairing whose sender then messages again
- **THEN** the message dispatches a turn normally

#### Scenario: Dismissed pairing denies

- **WHEN** the user dismisses a pending pairing and that sender messages again
- **THEN** the sender remains unauthorized and a new pending pairing is offered

### Requirement: Persistent chat sessions

Each allowlisted DM chat SHALL map to one persistent session: the chat's first message creates a session and every later message submits to the same session, so conversation context carries across messages. The session SHALL be created through the runtime's normal session creation, titled from the chat, and every turn it runs SHALL be stamped with the telegram origin so prompts, hooks, and memory treat it as a channel turn. The session is an ordinary session: the desktop UI can open it, watch it live, and reply in it.

#### Scenario: Context carries across phone messages

- **WHEN** an allowlisted user sends a second message in a chat that already has a session
- **THEN** the turn runs on the same session and the agent can refer to the earlier exchange

#### Scenario: Channel turn is origin-stamped

- **WHEN** a turn runs from a Telegram message
- **THEN** hooks and the turn's environment block report the telegram origin, not the user origin

### Requirement: Initiator delivery rule

Reply delivery follows the turn's origin: a turn initiated from Telegram SHALL deliver its output to that chat; a turn initiated from the desktop UI (including replies typed into the same session) SHALL NOT be pushed to the chat. Scheduled runs do not deliver unless the home chat announcement applies.

#### Scenario: Desktop reply stays in the desktop

- **WHEN** the user types a reply into the chat's session from the desktop UI
- **THEN** the response appears in the desktop transcript and no Telegram message is sent

#### Scenario: Channel reply reaches the chat

- **WHEN** a turn initiated from Telegram completes
- **THEN** the assistant's reply is delivered to that chat

### Requirement: Per-chat queueing

Messages arriving for a chat whose session is mid-turn SHALL queue per chat in arrival order and submit when the running turn finishes; a burst of rapid messages SHALL merge into one queued submission so multi-part sends arrive whole. The commands `/stop` and `/new` SHALL bypass the queue and act on the running turn or session immediately. Queueing MUST NOT lose a message: while the connection drops, queued and in-flight inbound messages are held and dispatched after reconnect.

#### Scenario: Busy chat queues

- **WHEN** a second message arrives while the chat's first turn is still running
- **THEN** it is not rejected; it submits automatically when the first turn finishes

#### Scenario: Stop bypasses the queue

- **WHEN** `/stop` arrives while a turn is running with queued messages
- **THEN** the running turn cancels immediately without waiting for the queue

### Requirement: Markdown delivery formatting

Replies SHALL be converted to Telegram MarkdownV2 from the agent's GitHub-flavored output: fenced code, bold, italic, links, quotes, and tables. A message Telegram rejects as malformed SHALL be resent as stripped plain text rather than dropped. Length chunking SHALL be computed on the converted text at Telegram's 4096 UTF-16-code-unit limit, prefer breaking at line or word boundaries, never split inside inline code, and re-open fenced code blocks across chunks with a part indicator.

#### Scenario: Unparseable markdown falls back to plain

- **WHEN** the converted MarkdownV2 is rejected by Telegram
- **THEN** the same content is delivered as stripped plain text

#### Scenario: Long reply chunks

- **WHEN** a reply exceeds the length limit
- **THEN** it arrives as ordered parts with code fences intact across the break

### Requirement: Live reply updates

While a turn streams, the channel SHALL keep the chat's message current by editing it at a bounded interval, and SHALL finalize with a last edit (or fresh message when the stream ran long) when the turn completes. Edit pacing and sends SHALL share one per-chat rate slot: sends wait for the slot, interim edits may be skipped, and the final edit is never gated. Identical truncated previews SHALL NOT be re-edited. While a turn runs, the chat SHALL show a typing indicator.

#### Scenario: Reply streams then finalizes

- **WHEN** a turn's output streams over several seconds
- **THEN** the chat's message updates periodically and ends with the complete reply

#### Scenario: Pacing protects the flood budget

- **WHEN** edits would exceed the shared per-chat rate slot
- **THEN** interim edits are skipped rather than sent, and the final content still lands

### Requirement: Flood handling

A Telegram rate-limit refusal longer than five seconds MUST NOT be slept off inline: the send SHALL fail closed, the chat enters a short local cooldown during which new sends refuse locally, and the pending output is redelivered when the cooldown ends. Refusals of five seconds or less may be retried inline. The channel MUST never block inbound processing on a flood penalty.

#### Scenario: Long penalty fails closed

- **WHEN** Telegram answers a send with a multi-minute retry-after
- **THEN** inbound messages keep processing and the reply is redelivered after the cooldown

### Requirement: Update integrity

The channel SHALL deduplicate redelivered updates using durable per-bot receipts, so a restart or reconnect never processes the same message twice. Updates that arrive while the connection is down SHALL be held (bounded) and dispatched on reconnect — never silently dropped once their delivery has been acknowledged. On cold boot, pending updates that queued while the app was closed SHALL be acknowledged and discarded rather than replayed as turns.

#### Scenario: Redelivered update ignored

- **WHEN** Telegram redelivers an update the channel already processed
- **THEN** no duplicate turn is dispatched

#### Scenario: Cold boot drops the backlog

- **WHEN** the app starts after being closed overnight with queued updates waiting
- **THEN** the backlog is acknowledged and discarded, and only new messages dispatch turns

### Requirement: Connection liveness

The channel SHALL verify its connection periodically with lightweight probes that exercise both the send path and the poll path, detect a wedged poll consumer that reports connected but receives nothing, and rebuild the polling adapter with bounded backoff when a failure is retryable. An invalid or revoked token SHALL be surfaced as a channel error state (visible in the UI) rather than retried forever. A second consumer polling the same token SHALL be detected and resolved rather than fought over.

#### Scenario: Wedged polling recovers

- **WHEN** the transport reports healthy but no updates arrive for the stall threshold
- **THEN** the channel rebuilds its polling and resumes receiving

#### Scenario: Revoked token surfaces

- **WHEN** the token is rejected as invalid
- **THEN** the channel enters a persistent error state shown in the UI and stops retrying

### Requirement: Media inbound

Photos and documents sent by an authorized user SHALL be downloaded and ride the existing attachment pipeline into the turn, degrading per the attachment rules when the effective model cannot accept them. A failed or oversized download SHALL both notify the chat and record an agent-visible note in the turn — never a silent empty turn.

#### Scenario: Photo joins the turn

- **WHEN** an allowlisted user sends a photo with a caption
- **THEN** the turn runs with the caption as text and the image as an attachment

#### Scenario: Failed download is visible

- **WHEN** a media download fails or exceeds the size cap
- **THEN** the chat is told and the turn's transcript carries a note about it

### Requirement: Group chats

A group chat MAY be allowlisted by chat ID. In an allowlisted group, messages trigger the agent only when addressed: an @mention of the bot (including commands addressed to it), a reply to the bot's message, or — when the chat is marked free-response — any message. Messages from senders not individually allowed MUST NOT trigger turns even in an allowlisted group. Messages that mention a different bot MUST be ignored. Unaddressed group chatter MAY be recorded into the session transcript without dispatching a turn, so the agent keeps context without waking.

#### Scenario: Mention triggers, chatter does not

- **WHEN** an allowlisted group receives an @mention and later an unaddressed message
- **THEN** the mention dispatches a turn and the unaddressed message does not

#### Scenario: Foreign bot mention ignored

- **WHEN** a group message mentions a different bot and not this one
- **THEN** no turn is dispatched

### Requirement: Control commands

An authorized sender SHALL be able to run `/new` (start a fresh session for the chat), `/stop` (cancel the running turn), and `/status` (report the agent, model, and busy state) as chat commands. A pending approval SHALL be resolvable from the chat via inline buttons wired to the runtime's decision path (see chat-approvals). Files the turn produced as artifacts SHALL be delivered to the chat as documents with their names.

#### Scenario: New session on command

- **WHEN** the user sends `/new` in a chat with an existing session
- **THEN** the next message runs on a fresh session and the old session remains in the desktop rail

#### Scenario: Approval from the chat

- **WHEN** a turn parks on a dangerous-command approval and the user taps Approve in the chat
- **THEN** the command executes and the turn continues as if approved in the desktop UI

### Requirement: Per-chat agent override

A chat MAY pin a specific agent; unpinned chats use the gateway's default agent. The override SHALL be settable from the Channels pane and visible via `/status`. A turn MUST NOT run when neither the chat override nor the default agent resolves to an existing agent.

#### Scenario: Pinned agent answers

- **WHEN** a chat pins agent B while the gateway default is agent A
- **THEN** messages from that chat run on agent B

### Requirement: Home chat announcements

A configured home chat SHALL receive a short result summary when a scheduled run's turn completes — the schedule name, the outcome, and a brief excerpt of the final reply. Announcements are best-effort: a failed announcement MUST be logged and MUST NOT affect the scheduled run's recorded outcome. When no home chat is configured, scheduled runs behave exactly as today.

#### Scenario: Schedule result announced

- **WHEN** a scheduled run completes with a home chat configured
- **THEN** the home chat receives a summary naming the schedule and the result excerpt

#### Scenario: Announcement failure is contained

- **WHEN** the announcement send fails
- **THEN** the scheduled run's outcome and next fire are unaffected

### Requirement: Channels settings surface

The Channels pane SHALL let the user enable the channel, set the bot token (write-only per credential secrecy), review connection state (including error states), resolve pending pairings, manage the per-chat agent overrides, and pick the home chat among the chats the channel knows. The pane SHALL present channel state legibly in both app themes.

#### Scenario: First-time setup

- **WHEN** the user enables the channel, pastes a token, and approves their own pairing
- **THEN** the pane shows the channel connected and the user's chat appears for override and home-chat selection

#### Scenario: Error state visible

- **WHEN** the token is revoked
- **THEN** the pane shows the channel in an error state with the reason
