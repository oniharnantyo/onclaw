## Why

OnClaw agents are only reachable through the web app and the `/v1` API. Peer platforms (OpenClaw, Hermes) treat Telegram as the primary front door — on-call engineers and mobile users expect to talk to their agents where they already are. A Telegram gateway closes the highest-impact ingress gap while leaving OnClaw's channel/room layer untouched.

## What Changes

- New **Telegram gateway** (`internal/gateways/telegram`) that bridges Telegram into existing agent sessions — **never into channels, the chokepoint, or work sessions**:
  - **Pairing**: workspace members pair their Telegram identity via a one-time expiring token (`/link`); runs always execute under the paired member's identity and RBAC. Unpaired senders are refused.
  - **Routing (two rules only)**: a direct message routes to the sender's default agent (per-user choice, falling back to the bot config's default agent) as a private session; a group chat routes to the one agent bound to that group as a shared session. One agent per group; no channel mapping, no forum-topic mapping, no ambient listening (Telegram privacy mode stays ON).
  - **Session keys**: deterministic `tg_dm_<telegram_user_id>_<agent_id>[_<n>]` and `tg_group_<chat_id>_<agent_id>` bindings; `/new` archives and suffix-bumps. Sessions persist like web sessions.
  - **Streaming renderer**: debounced message edits (~1.2 s), typing heartbeat, GFM→Telegram-HTML conversion with stack-based tag auto-closing, 4,096-char chunk splitting that cleanly closes/reopens code fences, and a parse-failure fallback to plain text.
  - **Approval bridge**: `approval_required` interrupts render as inline-keyboard approve/deny cards; button callbacks resume via the existing `Runner.Resume` path with the decision recorded.
  - **Ingress**: text, photos/documents (normalized into the existing 3-lane attachment pipeline), and voice notes (transcribed to text before the turn).
  - **Reliability**: delivery outbox with at-least-once redelivery after restart, per-chat bot-loop guard, adapter circuit breaker (auto-pause, manual resume), `migrate_to_chat_id` auto-remap, busy-input queueing (a message arriving mid-run becomes the next turn).
- New **admin surfaces**: a workspace settings Gateways pane (bot token, default agent, group bindings, enable/disable) and a member-facing pairing modal with expiry countdown.
- **BREAKING**: none. The gateway is additive; nothing about channels, `/v1`, or the web chat changes.

## Capabilities

### New Capabilities
- `telegram-gateway`: The full Telegram ingress — pairing and identity, chat bindings to agents, DM/group session semantics, streaming rendering, approval bridge, attachment/voice ingress, delivery outbox, lifecycle guardrails (loop guard, circuit breaker, chat-id migration), and the admin/pairing UI.

### Modified Capabilities
- `agent-runtime`: Gateway group sessions are excluded from the per-user session index (scheduler-session precedent); gateway DM sessions register under the paired member per the existing contract. A new origin value `telegram` rides the existing run-event and hook-payload origin contracts.
- `web-app/settings`: A new Gateways pane in workspace settings (bot connection, default agent, group bindings, pairing link flow) replacing the mock integrations row for Telegram.

## Impact

- **Code**: new `internal/gateways/` (service, router, streamer, renderer, outbox, adapters/telegram), new store port + postgres/fake implementations, new handlers under `internal/server/handlers/gateways.go`, wiring in `internal/cli` → router composition root; small runner touch for origin tagging and session-index exclusion.
- **Schema**: one migration adding `workspace_gateways`, `gateway_user_links`, `gateway_pairing_tokens`, `gateway_chat_bindings` (agent-scoped, unique per platform chat).
- **APIs**: workspace-scoped REST endpoints for gateway config, bindings, pairing tokens, and the Telegram webhook ingress path (long-polling runs in-process; no new public API contract beyond the admin endpoints).
- **Dependencies**: a Go Telegram Bot API client (long polling + webhook + inline keyboards); a speech-to-text dependency for voice notes (provider-pluggable, fail-soft when unconfigured).
- **Ops**: webhook mode needs a public HTTPS URL; long-polling mode works behind NAT — both supported, configurable per gateway.
