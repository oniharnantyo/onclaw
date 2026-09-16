## Why

OnClaw pins one gateway identity per platform (`UNIQUE(workspace_id, platform)`), so agents can only be distinguished after a message arrives (default agent, `/bind`, `/agent`). OpenClaw shows the stronger model: the bot itself is the agent selector — DM @atlas_bot and Atlas answers. Workspaces that want per-agent bots (one persona per team, per function, per audience) cannot express it today.

## What Changes

- **BREAKING** Gateway accounts become plural: a workspace MAY hold N gateway accounts per platform (Telegram bot tokens, WhatsApp linked accounts). The one-per-platform uniqueness constraint is dropped in favor of per-account identity.
- Each gateway account binds to exactly **one agent** (required at save): DMing that bot opens that agent's session. The gateway-level `default_agent_id` is retired — migrated into the first account's agent on upgrade, then gone.
- Group bindings gain an owning account (`gateway_id`): one bot per group remains the rule (the per-group uniqueness stays global); adding a second workspace bot to a bound group is a warned misconfiguration, not supported routing. Mention-routing between same-group bots is explicitly out of scope.
- Public webhook ingress moves from per-workspace to per-account routes (`/webhooks/telegram/:gatewayId`) with a per-account secret — Telegram requires a distinct webhook per token anyway.
- Pairing/user links stay platform-level identity: pairing once with any workspace bot links the member's platform identity workspace-wide (a human is one human to every bot).
- Lifecycle manager, streamer, and adapters run per account (one polling loop or webhook receiver per token); status, circuit breaker, enable/disable, and test message become per-account.
- Web UI (builds on the `revamp-gateways-sidebar` frame): platform sections render one row per account plus an `＋ Add a bot` / `＋ Add an account` affordance; the connect wizard gains a mandatory "agent this bot speaks for" step; the detail pane becomes per-account (token, agent, transport, enable, status, test).
- Implementation lands after and on top of `revamp-gateways-sidebar`; Telegram and WhatsApp both get multi-account support in this change.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `telegram-gateway`: one-to-many gateway accounts per workspace, per-account agent binding and routing, per-account transport/webhook ingress, retired `default_agent_id`, per-account pairing ingress with platform-level identity links.
- `web-app/settings`: Gateways pane rows-per-account with Add affordances, per-account detail pane, connect wizard agent step.

## Impact

- Schema: `workspace_gateways` (drop UNIQUE(workspace_id, platform); add identity + `agent_id`), `gateway_chat_bindings` (+ `gateway_id`), migration backfilling the existing row into the first account.
- `internal/gateways/` (lifecycle manager, router, streamer): per-account reconciliation instead of per-(workspace, platform).
- `internal/server/handlers/gateways.go` + routes: account-scoped CRUD (`/gateways/telegram/:gatewayId`), webhook ingress re-pointed, bindings carry account.
- Web: `GatewaysPane.tsx` sidebar rows, connect wizard, detail re-parenting; `api.ts` gateway client re-shaped to lists.
- Sessions: keys unchanged (`tg_dm_<uid>_<agent>`); heartbeat unaffected (per agent).
