# Proposal: add-telegram-channel

## Why

buddies is a tray gateway meant to accept triggers from cron, webhooks, and chat — but cron is the only trigger that exists, and there is no way to reach an agent away from the desk. A Telegram bot turns the phone into a full agent surface (conversations, approvals, schedule results) while the gateway keeps running in the background. Hermes — the reference harness — implements exactly this as its most battle-tested platform adapter; its mechanics (flood pacing, update receipts, liveness probes, layered authorization) are absorbed here as design constraints rather than relearned.

## What Changes

- **New Telegram channel** (`internal/channels/telegram`): a gateway-attached long-polling Bot API client built on a thin raw HTTP client (`net/http`, zero new Go dependencies), lifecycle-managed exactly like the scheduler (`Attach(gw)` start-on-Running / hold-on-Paused / stop-on-Stopped, `Stop()` in `App.Close`).
- **Authorization**: default-deny user allowlist with a pairing flow — `/start` from an unknown user makes the bot echo their numeric ID, and an approval card in the Channels pane adds them. Group chats are separately allowlisted with mention gates.
- **Session mapping**: one persistent session per DM chat (context carries across messages from the phone), created and submitted through the existing `Runs` seam (`CreateSession` + `Submit` with the new `OriginTelegram` origin). The channel owns a per-chat FIFO queue for messages arriving mid-turn, with `/stop` and `/new` running inline as bypasses.
- **Outbound delivery**: GFM→Telegram MarkdownV2 conversion with a stripped-plain resend on parse rejection, chunking at 4096 UTF-16 code units computed post-conversion (fenced blocks re-opened across chunks), replies streamed via throttled `editMessageText` under a shared per-chat send+edit pacing slot (sends wait, interim edits skip, final edit never gated), flood waits >5s fail closed with a local cooldown, typing indicator while a turn runs.
- **Resilience**: durable per-bot update receipts (dedupe against Bot API redelivery), hold queue across live reconnects (events are never dropped once the offset is acked), pending updates dropped on cold boot, heartbeat liveness probes (`get_me` + `pending_update_count`) with supervised adapter rebuild and backoff.
- **Rich inbound**: photos and files ride the existing attachments pipeline (`AttachmentKeys`); group chats with a chat allowlist, `require_mention` / reply-to-bot / @mention gates, and exclusive-bot-mention filtering.
- **Control surface**: slash commands over chat (`/new`, `/stop`, `/status`); pending approvals resolved via Telegram inline buttons wired to `ResolveApproval`; turn artifacts delivered back to the chat as documents; per-chat agent override with the reserved `gateway_settings.default_agent_id` as fallback.
- **Home chat**: a configured home chat receives a short result summary when a scheduled run's turn completes — the channel's first outbound-initiated delivery and the cron/Telegram convergence.
- **Frontend**: a Channels pane in the settings INTEGRATIONS section (token write-only with redacted hint per the provider-credential pattern, enable state, pairing approvals, per-chat bindings, home chat picker); Telegram-origin sessions identifiable in the session UI.
- **Deferred**: voice notes (needs an STT provider — separate change with its own dependency decision), forum topics, cross-platform mirroring, webhook mode, managed-bot onboarding, local Bot API server mode.

## Capabilities

### New Capabilities

- `channels`: the Telegram channel — transport and lifecycle, authorization and pairing, session mapping and queueing, outbound formatting/delivery/pacing, resilience, media inbound, control surface, home chat announcements, and the Channels settings surface.

### Modified Capabilities

- `chat-approvals`: adds a requirement for a remote decision surface — a pending approval MAY be resolved from an authorized external channel through buttons carrying the pending interrupt identity, with stale decisions conflict-failing exactly as the bound call does.
- `settings`: the settings-nav sidebar requirement gains Channels under INTEGRATIONS.

Not modified, deliberately: `agent-runtime` — the seam a channel needs (`CreateSession`, `Submit` with origin, `Subscribe`, `Busy`, `ResolveApproval`) is already specified there; `OriginTelegram` is a new value in the origin vocabulary that `agent-prompts` and `agent-hooks` already treat generically. `data-store` needs no delta: channel tables follow the existing aggregate pattern (as `schedules` did) with their storage requirements stated in `channels`.

## Impact

- **Backend**: new `internal/channels/telegram` package; new SQLite tables (channel config, pairings/allowlist, per-chat session bindings, update receipts) via forward-only migration; `hooks.OriginTelegram` constant (the origin file already reserves it); `Assemble` construction + `App.Close` teardown; scheduler completion announcement hook.
- **Frontend**: Channels pane (`PaneKey`, section entry, component); session origin affordance in the chat/session UI.
- **Dependencies**: none — the Bot API is called directly over `net/http`.
- **Systems**: long-polling outbound HTTPS to api.telegram.org from the tray process; no inbound network listener (webhook mode deferred).
