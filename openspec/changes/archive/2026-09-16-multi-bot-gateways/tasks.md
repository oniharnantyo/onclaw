## 1. Schema and store

- [x] 1.1 Create migration 0000xx: drop UNIQUE(workspace_id, platform) on workspace_gateways; add identity + agent_id + UNIQUE(workspace_id, platform, identity); backfill first account from the existing row (identity = bot_username, agent_id = old default_agent_id or first-workspace-agent fallback with status_error note); backfill gateway_chat_bindings.gateway_id from the platform's sole account; drop default_agent_id — with down migration restoring column + constraint (first account preserved)
- [x] 1.2 Extend store layer: list accounts per (workspace, platform), get/upsert/delete per account id, per-account enable flag and status; fake + postgres tests including multi-row uniqueness (same workspace two bots; two workspaces same bot name)

## 2. Routing and runner

- [x] 2.1 Domain validation: account requires bound workspace agent; identity non-empty; retirement of default_agent_id; DM routing resolves to the account's agent (per-user override removed from gateway surfaces); tests
- [x] 2.2 Chat bindings carry gateway_id with global per-group uniqueness; second-workspace-bot-in-bound-group detection surfaces a warning code; tests
- [x] 2.3 Lifecycle manager reconciles a list: per-account polling loop / webhook receiver, per-account semaphore, status_error + circuit breaker per account; restart-drain tests
- [x] 2.4 Session keys unchanged (`tg_dm_<uid>_<agent>`); regression tests proving two bots → two sessions for one member and no cross-contamination

## 3. API surface

- [x] 3.1 Re-shape routes: list + create under `/gateways/telegram`, get/put/enable/disable/test/delete under `/gateways/telegram/:gatewayId`; webhook ingress re-pointed to `/webhooks/telegram/:gatewayId` with per-account secret; whatsapp equivalents; handler tests (auth, fielded validation, account not found)
- [x] 3.2 Pairing: tokens scoped to workspace+platform (not account); link flows prove one-link-spans-all-bots and platform-wide unlink; tests
- [x] 3.3 `/agent` DM command retires with a hint reply pointing at the workspace's other bots; binding command addressing follows the owning bot; update command tests

## 4. Web

- [x] 4.1 api.ts: gateway clients return account lists; per-account mutations; types updated (client tests)
- [x] 4.2 Sidebar: platform sections render one row per account + `＋ Add a bot` / `＋ Add an account`; dot vocabulary per account; detail pane re-parents per account with agent picker (changeable), token replace, transport, enable, test
- [x] 4.3 Connect wizard: mandatory agent step (picker) before save; validation errors fielded; component tests
- [x] 4.4 Bindings list gains owning-bot column; unbound-second-bot warning surfaces in pane; mobile chip row carries N accounts (360px overflow check)

## 5. End-to-end verification

- [x] 5.1 smoke.sh: migration path (pre-existing gateway becomes account 1 with correct agent); add second bot; DM each bot → correct agent sessions; group bound to bot A ignores bot B; webhook ingress segregation; disable one account while the other serves
- [ ] 5.2 Manual pass: live two-bot workspace (two BotFather tokens) — DM routing, group binding, pairing once → both bots, pane rows/dots, wizard agent step
