# Design: add-telegram-channel

## Context

The runtime already exposes the full seam a channel needs: `agents.Manager.CreateSession` / `Submit(SendRequest{Origin})` / `Subscribe` / `Busy` / `Cancel` / `ResolveApproval`; `hooks.OriginTelegram` is reserved in the origin vocabulary and the environment block, operating contract, and memory origin mapping key off origin generically (`internal/agents/hooks/origin.go`, `turninput.go:163`, `contract.go:92`). The scheduler established the gateway-attachment pattern (`internal/scheduler/ticker.go:35`) and the `Runs` seam (`internal/cli/schedule_runs.go`); `gateway_settings.default_agent_id` is persisted but unused, documented as reserved for channel triggers. `Submit` rejects concurrent turns with `ErrTurnInProgress` — queueing is a caller concern (chat-approvals spec already words it that way). There is no webhook receiver; the gateway doc names "cron, webhook, Telegram" as future adapters. Hermes' Telegram adapter (`~/.hermes/hermes-agent/plugins/platforms/telegram/`, ~8.8k LN) is the reference; the ratio lesson — ~2.5k LN transport, ~5k LN failure handling — drives what we adopt vs. cut. See explore memory `buddies-telegram-explored` for the full Hermes survey.

## Goals / Non-Goals

**Goals**: one Telegram bot channel with the full behavior set in the channels spec (authz/pairing, persistent chat sessions, queueing, MarkdownV2 delivery with streaming edits, flood/pacing discipline, receipts/liveness resilience, media inbound, groups, commands/approvals/artifacts, home chat, Channels pane).

**Non-Goals**: voice notes/STT (separate change + dep decision); forum topics; cross-platform mirroring; webhook receive mode; managed-bot hosted onboarding; local Bot API server mode; multiple simultaneous bots (one channel row today, table shaped so more can exist); a generic multi-platform adapter abstraction.

## Decisions

**D1 — Concrete package, scheduler lifecycle.** New `internal/channels/telegram` (service + `botapi` client + `markdown` conversion subpackage is fine within it), constructed in `Assemble` after the runtime, `Attach(gw)` copying `ticker.go:35` (poll goroutine starts on `StateRunning`, holds on `StatePaused`, stops on `StateStopped`), `Stop()` in `App.Close`. Server mode gets it for free through shared `Assemble`. *Alternative rejected*: a Hermes-style `BasePlatformAdapter` registry in `internal/channels` — no second platform exists; extract the abstraction when one lands (the gateway doc already anticipates "trigger adapters").

**D2 — Thin raw Bot API client.** Hand-written `botapi` over `net/http`: `getUpdates` (long poll, offset, ~50s timeout), `sendMessage`, `editMessageText`, `sendChatAction`, `sendDocument`, `getFile`, `answerCallbackQuery`, `getMe`, `getWebhookInfo`, `setMyCommands`. Minimal response structs for exactly the fields consumed. *Alternative rejected*: tgbotapi/telebot — the transport is the small part, they add a dependency for little, and raw HTTP keeps the parse surface auditable. Optional HTTP(S)/SOCKS proxy URL on the channel config honored via `http.Transport` (Telegram reachability varies by network; Hermes treats this as table stakes).

**D3 — Storage, migration 000018.** Tables following the aggregate pattern (`schedules` precedent; no data-store delta needed):
- `channels` — `id` (slug, `telegram` today), `enabled`, `bot_token` (in the clear per the `providers.api_key` precedent, `store/types.go:84`), `proxy_url`, atomic changes.
- `channel_users` — the DM allowlist: `telegram_user_id` (unique), display name, added time.
- `channel_chats` — per-chat state: `chat_id` (unique), kind (`dm`|`group`), title, `session_id` (persistent mapping, restart-safe), `pinned_agent_id` (nullable), `is_home` (one row max), `free_response` (groups).
- `channel_pairings` — pending: `telegram_user_id`, display name, requested time; cap enforced in service.
- `channel_update_receipts` — `(bot_username, update_id)` processed marker, created time; service prunes >24h lazily.
Access via a new `store` port + sqlite impl; CRUD surfaced through a `ChannelService` (Wails-bound).

**D4 — Authorization and pairing.** Default deny. DM: sender's user id must be in `channel_users`. Unknown sender → reply with their numeric id + create a pending pairing (cap 5 per user, service-enforced); the UI approves/dismisses via `ChannelService`. Groups: chat must exist in `channel_chats` with kind=group; sender must also be allowlisted; trigger only on @mention (incl. `/cmd@bot`), reply-to-bot, or `free_response`; messages mentioning another bot are ignored. Callback taps (approval buttons) re-verify the tapper's user id against the allowlist. Fail closed on any lookup error. *Alternative rejected*: Hermes' 8-char pairing codes — we have a desktop UI to approve in; echoing the numeric ID is simpler and cannot be phished through a third party.

**D5 — Session mapping and queueing.** `channel_chats.session_id` pins the persistent session (created on first message via `CreateSession` + `SetTitle("<chat title> — Telegram")`); `/new` clears the binding (old session stays in the rail). Submission always `Submit(SendRequest{SessionID, Message, Origin: OriginTelegram})`. Per-chat FIFO queue lives in the channel service (in-memory): `Submit` returning `ErrTurnInProgress` enqueues; a turn-completion subscription (or the app-level `turns:changed` callback already wired in `app.go:146`) dequeues and submits the next. Inbound text debounces ~600ms so multi-part pastes merge (Hermes uses 0.18–0.24s adaptive; 600ms is one constant, not an adaptive ladder). `/stop` and `/new` check `Busy` and act immediately, bypassing the queue.

**D6 — Delivery pipeline.** After a successful Submit, subscribe to the session (`manager.Subscribe`) and pump events (the `ChatService.forward` pattern, `chat.go:374`): accumulate `text_delta`; every ~2s push an `editMessageText` preview (Hermes measured 0.8s causing 83% of flood penalties — we start at 2s, constant, tunable); on `assistant_message` finalize with the last edit — or a fresh `sendMessage` when the stream ran longer than 5 minutes (edit timestamps go stale, Hermes' `fresh_final_after_seconds` rule); `turn_error`/`turn_cancelled` send a short notice. Deliver only for turns with `OriginTelegram` (initiator rule); the subscription is released at turn end.

**D7 — MarkdownV2 conversion.** `markdown.go` implements GFM→MarkdownV2: fenced code captured verbatim then internally escaped, bold/italic/strike/spoiler/quotes, links `[text](url)` with URL escaping, tables flattened to bullet groups, a bare `(){}'`-outside-code safety-net escape. Sends are MarkdownV2-first; a Telegram 400 parse rejection resends stripped plain text (same rule on edits and final sends). Chunking runs on the **converted** string at 4096 UTF-16 code units (`utf16.RuneCount`-style accounting), preferring newline/space breaks, never splitting inline code, closing and re-opening fences across parts, `(1/n)` suffix escaped. Unit tests carry a conversion + chunking table ported from Hermes' documented behaviors.

**D8 — Pacing and flood.** Per-chat single-slot pacer shared by sends and edits (1s); sends wait for the slot, interim edits skip when the slot is busy, final edits never gate. `retry_after` ≤5s sleeps inline; >5s fails closed — chat enters a local cooldown (capped 300s) during which sends refuse locally, and pending output redelivers after the cooldown. Nothing on the inbound path ever sleeps on a flood penalty (the Hermes 97-minute-freeze lesson).

**D9 — Update integrity.** `processUpdate` first checks the in-memory seen-set then `channel_update_receipts` (Bot API redelivers unacked updates ≤24h). While disconnected, admitted events hold in a bounded (64) queue and dispatch on reconnect. On process start, the first `getUpdates` passes the current offset (i.e. confirms the last receipt) — cold-boot backlog is acked-and-dropped; reconnects within a process never advance past unheld work.

**D10 — Liveness.** *(Amended at apply time, task 5.3: the loop re-reads its row only at (re)start, so configuration saved after boot never reached a live or idle runtime — a channel configured post-boot stayed silent forever, and an absent row at boot even dead the attachment terminally. Now: connect reads an absent or tokenless row as idle, a disabled/idle exit clears the loop's start sentinel, `Service.ConfigChanged` restarts the runtime on an operator save or toggle — clearing even a terminal failure, since a corrected token pasted after a 401 is operator action, not a retry fight — and `ChannelService.OnChange` (wired in `Assemble`) fires the kick on connection-affecting mutations.)* A 90s heartbeat goroutine: `getMe` (send path proof) and `getWebhookInfo().pending_update_count` (poll path proof — a wedged long poll reports healthy while deaf) plus a no-round-trip stall timer. Confirmed stall or retryable network error → rebuild the polling loop with 30s→300s backoff. HTTP 409 → another consumer owns the token (second app instance): stop, surface error state, no retry fight. 401/404 on token calls → persistent error state. All states emit on a `channel:changed` app event (registered in both `main.go` and `main_server.go` like `EventSchedulesChanged`) so the pane shows connected/degraded/error live.

**D11 — Media inbound.** Photo (largest size ≤20MB) / document → `getFile` → download to a temp file → register through the existing attachments store (the same registry `Submit`'s `AttachmentKeys` must name), then submit with the caption as text. Capability degradation, per-message caps, and pointer-note fallbacks are the existing attachments behavior — no new policy. Failed/oversized download → chat notice + agent-visible note in the turn (mirrors Hermes' never-silent rule).

**D12 — Approvals, commands, artifacts.** The delivery subscription observes `approval_required`; the bot sends the command text with an inline keyboard whose `callback_data` is `appr:<sessionID>:<interruptID>:<approve|deny>`; the tap path re-checks allowlist, then calls `manager.ResolveApproval` (all conflict semantics inherited from chat-approvals), and answers the callback. Commands parse before dispatch: `/new`, `/stop`, `/status` (agent, pinned override, model, busy state). `turn_completed`'s `Artifacts` list → send each file via `sendDocument` (cap 10 files/turn, skip >50MB with a note). `setMyCommands` runs post-connect as housekeeping (off the connect path).

**D13 — Home chat announcements.** `scheduleRuns` grows an optional `CompletionNotifier` seam (nil-safe, injected in `Assemble`): after `Submit`, a goroutine waits `Turn.Done()`, reads the terminal outcome + final `assistant_message` excerpt, and calls the channel's announcer, which formats "schedule name — outcome — excerpt (≤400 chars)" to the `is_home` chat through the normal outbound path (pacing, chunking, flood rules all inherited). Best-effort: errors log, never touch `RecordFire` outcomes. *Alternative rejected*: hook-based (`run_finished` script) — pushes an integration-critical behavior into user-configurable hooks.

**D14 — Frontend.** *(Amended at apply time, task 9.3: the pane is channel-agnostic —
`ChannelService.ListChannels` (port + sqlite + fake) drives one card per stored channel;
each card is a brand mark (the brandMarks grammar gains a monochrome `telegram` glyph),
display name, live state chip, and enable toggle over a per-channel body registry, so a
future channel is a registry entry plus a body component. A fresh install renders the
Telegram card from the same not-configured default the backend reads for an absent row.
The pane-level `PaneHeader` description was dropped — the settings shell owns the title
and description (the duplicated description line was a bug shared by the Skills, Hooks,
and MCP panes).)*  `PaneKey` += `"channels"`; Channels pane under INTEGRATIONS (`settingsPaneSections`, `paneComponents`) in `features/settings/components/ChannelsPane.tsx`: enable toggle, token field (write-only, starts blank, hint under it per `ProviderFormValues.apiKey` precedent), connection-state chip from `channel:changed`, pending-pairing cards (allow/dismiss), per-chat rows (pinned agent select, home radio, free-response toggle for groups). Session origin affordance: the transcript's user message payload for channel turns names the origin (verified at implementation; if the `user_message` payload lacks it, the turn's environment origin is the fallback signal) — a small Telegram glyph/badge in the session UI, both themes per the settings spec.

**D15 — Where Hermes' machinery was deliberately cut.** No adaptive batching ladder (one debounce constant), no draft streaming (Bot API 9.5 drafts are DM-only and new), no DM-topic mode, no message-reaction feedback, no standalone out-of-process sender, no DoH IPv4 fallback transport (Go's dialer + optional proxy covers the realistic cases; keepalive defaults suffice). Each is a known re-addable if a real need appears.

## Risks / Trade-offs

- [MarkdownV2 escaping is fiddly and Telegram's parser is strict] → plain-text fallback on every send/edit path + a conversion test table; worst case is ugly-but-delivered text, never a dropped reply.
- [Streaming edits can still earn flood penalties] → 2s interval start, shared slot, skip-interim rule, fail-closed ledger-style redelivery; interval is a constant, cheap to retune.
- [Long-poll goroutine leak on teardown] → every network op carries a context; `Stop()` cancels then joins with a bounded wait, per Hermes' bounded-teardown rule.
- [Token stored in the clear] → accepted by existing precedent (`providers.api_key`), single-user local app; write-only surface + hint keeps it out of the UI.
- [Two app instances fight over the token] → 409 detection stops our poller and shows an error state instead of retrying (Hermes' conflict rule).
- [Queue lives in memory] → a crash loses queued (not yet submitted) messages; acceptable — receipts guarantee no *duplicate* turns, and the desktop rail shows the session history.

## Migration Plan

Forward-only migration 000018 creates the five tables; no existing tables change. Rollback = disable the channel (row remains inert). Nothing else reads the new tables, so shipping is additive; `Assemble` wires the service nil-safe when no channel row exists (fresh installs and upgrades both boot clean).

## Open Questions

None blocking. Two implementation-time verifications that do not change specs or tasks: whether the `user_message` event payload carries origin for the D14 badge (fallback signal exists), and the exact attachments-registration call shape for D11 (the port exists; the glue is mechanical).
