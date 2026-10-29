# Tasks

## 1. Foundation: origin, storage, service surface

- [x] 1.1 Add `OriginTelegram = "telegram"` to `internal/agents/hooks/origin.go` (the file reserves the value) and extend the origin documentation comment; verify `go test ./internal/agents/...` and that the turn environment block renders the telegram presence line for a channel-origin turn (unit test in `turninput` tests).
- [x] 1.2 Write migration 000018 creating `channels`, `channel_users`, `channel_chats`, `channel_pairings`, `channel_update_receipts` per design D3; verify `go test ./internal/store/sqlite/...` applies it forward on a fresh DB and over a populated 000017 DB.
- [x] 1.3 Add the store port + sqlite impl for the five aggregates (CRUD, atomic channel upsert, unique chat id, single-home constraint, receipt put/prune); verify with round-trip tests in `internal/store/sqlite` including the one-home and unique-user constraints.
- [x] 1.4 Add `ChannelService` (Wails-bound): get/save channel (token write-only, blank-keeps, `TokenHint` last-4), enable/disable, list chats + set pinned agent / home / free-response, list + resolve pairings; register events `channel:changed` + `channel:state` in both `main.go` and `main_server.go`; verify via service tests that reads never return the token.

## 2. Bot API client

- [x] 2.1 Implement `internal/channels/telegram/botapi` over `net/http`: `getUpdates` long poll with offset/timeouts, `sendMessage`, `editMessageText`, `sendChatAction`, `sendDocument`, `getFile`, `answerCallbackQuery`, `getMe`, `getWebhookInfo`, `setMyCommands`; minimal response structs; optional proxy from channel config via `http.Transport`; every call context-carrying; verify with `httptest` fixtures covering success, 400/401/403/409/429-with-retry_after, and network error classification.
- [x] 2.2 Add the typed send-error classifier (rate_limited / forbidden / not_found / transient / parse) with `retryAfter` extraction; verify with table tests.

## 3. Channel core: lifecycle, authz, pairing, sessions, queue

- [x] 3.1 Implement the gateway-attached service lifecycle per design D1 (`Attach`/pause-hold/stop, bounded teardown with context cancellation and a join deadline); verify with a fake gateway driving Running→Paused→Stopped and asserting poll start/stop, plus a teardown test with a hung HTTP handler.
- [x] 3.2 Implement the update loop: fetch → dedupe (memory set + receipts table) → dispatch → ack offset; verify with a scripted fake Bot API covering redelivery dedupe and offset advance.
- [x] 3.3 Implement authorization per design D4: default-deny allowlist check, unknown-DM pairing creation (cap 5, bot replies with the numeric id), group rules (chat allowlist + sender allowlist + mention/reply/free-response gates + foreign-bot-mention filter); verify with table tests over update fixtures for every allow/deny branch including callback-tap auth.
- [x] 3.4 Implement persistent chat sessions per design D5: first message creates + titles the session and pins `channel_chats.session_id`, later messages submit to it with `OriginTelegram`, `/new` clears the binding; verify with a fake runtime asserting CreateSession/Submit call shapes and restart-survival of the mapping via the store.
- [x] 3.5 Implement the per-chat FIFO queue: `ErrTurnInProgress` enqueues, turn-completion dequeues, rapid inbound text merges on a ~600ms debounce, `/stop` and `/new` bypass; verify with a fake runtime covering queue → drain order, merge, and bypass paths.

## 4. Outbound delivery

- [x] 4.1 Implement `markdown.go` GFM→MarkdownV2 conversion per design D7 (fences protected + escaped, bold/italic/strike/spoiler/quote/links, tables → bullets, safety-net escaping) plus the stripped-plain fallback renderer; verify with the conversion table tests (port Hermes' documented cases) and a parse-rejection → plain resend test against the fake Bot API.
- [x] 4.2 Implement UTF-16 chunking on converted text: 4096 cap, newline/word-preferred breaks, no inline-code splits, fence re-opening, escaped `(1/n)` markers; verify with table tests including CJK/astral-plane text and a fence spanning three chunks.
- [x] 4.3 Implement the per-chat shared pacing slot and flood handling per design D8 (sends wait, interim edits skip, final never gated; ≤5s inline retry; >5s fail-closed cooldown with redelivery); verify with a fake Bot API emitting 429s and asserting ordering, skip behavior, and cooldown redelivery without blocking inbound.
- [x] 4.4 Implement the delivery pump per design D6: subscribe on Submit, delta-accumulated preview edits every ~2s, finalize on `assistant_message` (fresh message past the staleness threshold), `turn_error`/`turn_cancelled` notices, initiator rule (deliver only `OriginTelegram` turns), unsubscribe at turn end; verify with a scripted event stream asserting edit cadence, final content, and that a user-origin turn on the same session sends nothing.
- [x] 4.5 Add the typing indicator while a turn runs (sendChatAction refresh loop, stopped at finalize); verify via the fake Bot API call log.

## 5. Resilience

- [x] 5.1 Implement the hold queue for updates admitted during disconnect (bounded 64, dispatch on reconnect) and cold-boot backlog drop (first poll confirms current offset); verify with the fake Bot API: disconnect → enqueue → reconnect drains in order; cold start with pending updates acks and drops them.
- [x] 5.2 Implement the liveness heartbeat per design D10 (90s `getMe` + pending_update_count probes, stall timer, poll-loop rebuild with 30s→300s backoff) and the terminal states (401/404 → error state; 409 → stop without retry), emitting `channel:state` transitions; verify with a fake Bot API that wedges getUpdates and asserts rebuild, and a 409/401 sequence asserting terminal states and event emission.
- [x] 5.3 Make post-boot configuration reach the runtime (apply-time amendment to D3/D10): connect reads an absent or tokenless row as idle — never terminal — a disabled/idle connect exit clears the loop's start sentinel, and `Service.ConfigChanged` restarts the runtime (clearing even a terminal failure, since an operator save is not an automatic retry); `ChannelService.OnChange` fires on SaveChannel/SetEnabled and Assemble wires it to the kick; verify with lifecycle tests (idle→kick→polling, live→disable→stops, terminal-401→corrected token→recovers) and a service test asserting kicks fire on connection mutations only.

## 6. Rich inbound

- [x] 6.1 Implement media inbound per design D11: photo (largest size) and document download via `getFile` → attachments-store registration → submit with caption; failure/oversize path notifies the chat and records the agent-visible note; verify with a fake Bot API serving file downloads and a fake attachments store asserting registration keys, plus the failure path.
- [x] 6.2 Implement group dispatch end-to-end (allowlisted group + mentioned message → turn on the group's session; unaddressed chatter optionally recorded per `free_response`=off as transcript-only notes when supported, else dropped with the decision recorded in code); verify with group update fixtures for mention/reply/chatter/foreign-bot branches.

## 7. Control surface

- [x] 7.1 Implement `/new`, `/stop`, `/status` command parsing and dispatch (bypass queue; status reports agent, pinned override, model, busy state); verify with command fixtures against a fake runtime including busy-session `/stop` and unknown-command pass-through-as-text.
- [x] 7.2 Implement approval buttons per design D12: observe `approval_required` on the delivery subscription, send the command card with `appr:<sessionID>:<interruptID>:<approve|deny>` callback data, resolve via `manager.ResolveApproval` after allowlist re-check, answer the callback; verify with a fake runtime covering approve, deny, unauthorized tap, and stale-tap conflict (asserts no state change).
- [x] 7.3 Implement artifact delivery: on `turn_completed` send up to 10 artifacts via `sendDocument`, skipping >50MB with a chat note; verify with a fake runtime emitting artifact-bearing completions against the fake Bot API.
- [x] 7.4 Implement per-chat agent override resolution (pinned agent → `gateway_settings.default_agent_id` → no-turn error surfaced to the chat); verify with store-fixture tests over all three resolution branches.

## 8. Home chat announcements

- [x] 8.1 Add the nil-safe `CompletionNotifier` seam to `scheduleRuns` (goroutine waits `Turn.Done()`, extracts outcome + final assistant excerpt ≤400 chars) and wire the channel announcer in `Assemble`; verify with a fake notifier asserting payload shape and that a missing home chat is a no-op.
- [x] 8.2 Verify announcement delivery rides the normal outbound path (pacing/chunking/flood inherited) and that a failed send is logged without touching `RecordFire` outcomes; verify via fake Bot API failure injection and scheduler store assertions.

## 9. Frontend

- [x] 9.1 Add the Channels pane: `PaneKey` += `"channels"`, INTEGRATIONS section entry, `ChannelsPane.tsx` with enable toggle, write-only token field + hint, connection-state chip fed by `channel:state`, pending-pairing allow/dismiss cards, per-chat rows (pinned agent select, home radio, free-response toggle); verify `cd frontend && npx vp check src` passes and component tests cover token-blank-keeps UX and pairing resolution.
- [x] 9.2 Add the channel-origin affordance in the session UI (Telegram badge on channel turns, both themes); verify component test asserts presence for telegram-origin events and absence for user origin, plus theme snapshots.
- [x] 9.3 Restructure the Channels pane around per-channel cards (brand mark + live state chip + enable toggle + per-channel body registry) fed by a new `ListChannels` store/service seam; drop the duplicated pane descriptions (shell owns title + description) and restyle the pane (quiet token save, guided empty states); verify `cd frontend && npx vp check src` passes and component tests cover the card header, disabled-state chip, and event-driven pairing refresh.

## 10. End-to-end verification

- [x] 10.1 Go gates green: `gofmt -w .`, `go vet ./...`, `go test ./...` across all packages including the new ones; fix any fallout in existing tests (scheduler/service wiring).
- [x] 10.2 Headless smoke (env-gated, skip by default per project convention): server-mode `Assemble` + a scripted fake Bot API driving the full loop — pairing → allow → message → queued message → streamed reply chunks → approval card → artifact send; assert transcript, receipts, and no duplicate dispatches.
- [ ] 10.3 Live smoke with a real bot token (env-gated): DM conversation with context carry-over, long-reply chunking, mid-turn queueing, `/stop`, approval button, photo inbound; record evidence under `.team/smoke/add-telegram-channel/`.
- [ ] 10.4 Frontend smoke: Channels pane setup flow (enable → token → pairing card appears on `/start` → approve → connected chip) against the dev server; both themes visually checked.
