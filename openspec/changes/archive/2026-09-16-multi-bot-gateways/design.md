## Context

`workspace_gateways` is UNIQUE(workspace_id, platform) — one bot identity per platform, Hermes-shaped. Routing distinguishes agents only after ingress (member per-user default → gateway `default_agent_id` → group binding). OpenClaw demonstrates the inverted model: one bot per agent as separate account entries, the receiving bot selecting the agent. This change adopts it, per the 2026-09-15 locks: one agent per bot, one bot per group, both platforms together. Lands after `revamp-gateways-sidebar` and re-parents its detail surfaces under rows; the sidebar frame itself is unchanged. See proposal.md — Why.

## Goals / Non-Goals

**Goals:**
- N gateway accounts per platform per workspace; the bot identity selects the agent at ingress.
- Per-account lifecycle: transport, webhook ingress, enable/disable, status, circuit breaker, test message.
- Zero-surprise upgrade: today's row becomes the first account and keeps working.

**Non-Goals:**
- Mention-routing between same-group bots (one bot per group stays; second bot in a bound group is warned, silent).
- Per-bot pairing ledgers (pairing stays platform-level identity).
- New platforms; channel-surface routing (Telegram⇄agent-sessions-only scope holds).
- Per-user DM agent overrides on gateway surfaces (the bot IS the choice; `/agent` in gateway DMs retires with `default_agent_id` — web chats keep it).

## Decisions

**D1 — Account entity, not a new table.** `workspace_gateways` gains `identity` (@bot_username / phone) and `agent_id`; the platform-uniqueness constraint is dropped for UNIQUE(workspace_id, platform, identity). Alternative (separate `gateway_accounts` table) rejected: the row already IS the account; a second table forces a join on every routing read for no modeling gain.

**D2 — One agent per account, required.** `agent_id` NOT NULL, validated against workspace agents at save. Replaces `default_agent_id` (column dropped; migrated into the first account). Per-user link agent overrides stop applying to gateway DMs — resolving them against the bot's agent would defeat bot-identity selection, and keeping both makes routing order a quiz.

**D3 — Bindings name their owner.** `gateway_chat_bindings` gains `gateway_id`; the UNIQUE(platform_chat_id) stays global (one bot per group is cross-account, not per-account). Second workspace bot in a bound group = configuration warning surfaced in the pane (lint-style), never silent dual-answering. Mention-routing deferred — it drags bot-loop guards and privacy-mode races into scope for marginal value while bots-per-agent exist.

**D4 — Ingress and lifecycle per account.** Webhook route becomes `/webhooks/telegram/:gatewayId` with a per-account secret (Telegram mandates distinct webhook URLs per token anyway; workspace-slug URLs would collide the moment two accounts exist). Lifecycle manager reconciles a list of accounts per workspace: one polling loop or one webhook receiver per token, per-account semaphore slot, status_error and breaker per account. Session keys stay `tg_dm_<uid>_<agent>` — with 1:1 bot⇄agent they are unambiguous by construction.

**D5 — Pairing stays platform-level.** `gateway_user_links` keeps UNIQUE(workspace_id, platform, platform_user_id): pairing with any bot links the identity workspace-wide. Alternative (per-bot links) rejected: a human is one human; per-bot links would force N pairings and N unlink flows for no security gain.

**D6 — Wizard and pane.** Connect wizard gains the mandatory agent step (agent picker before save). Sidebar platform sections render the account list + `＋ Add a bot` / `＋ Add an account`; detail re-parents under a row. WhatsApp multi-account: multi-device lane = one QR pairing session per added number; cloud-api lane = one credential set per row.

## Risks / Trade-offs

- [Migration drops a column (`default_agent_id`) and changes ingress routes] → Backfill runs in the up migration (first account inherits the old default agent); webhook URLs are re-registered per account at first sync after deploy; long-polling accounts are unaffected. Rollback is the down migration; re-up re-runs the backfill idempotently (accounts keyed by identity).
- [Two live polling loops per workspace double Telegram getUpdates pressure] → Per-account loops are the Telegram-sanctioned shape (each token polls independently); concurrency caps stay per account.
- [Users expect `/agent` in gateway DMs to still switch] → The bot's identity is now the switcher; the DM command retires with a hint reply pointing at the other bots. Documented in tasks as a copy pass.

## Migration Plan

0000xx: drop UNIQUE(workspace_id, platform) → add identity + agent_id + UNIQUE(workspace_id, platform, identity); backfill: existing row becomes the first account (identity = stored bot_username, agent_id = old default_agent_id — if NULL, the workspace's first agent as a last resort, flagged `status_error` = "pick an agent for this bot"); bindings backfilled with their platform's sole account id; drop `default_agent_id`. Deploy order: schema → server → web, one wave. Rollback: down migration restores the column and constraint (extra accounts are dropped, first account preserved).

## Open Questions

None — routing, grouping, platform scope, and change structure were user-locked 2026-09-15.
