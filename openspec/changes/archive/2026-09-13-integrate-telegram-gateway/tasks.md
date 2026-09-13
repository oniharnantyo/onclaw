## 1. Domain, schema, and store

- [x] 1.1 Add gateway domain entities in `internal/domain/gateways.go` (GatewayConfig, UserLink, PairingToken, ChatBinding, OutboxEntry) with validation sentinels (invalid token shape, binding conflict, expired token)
- [x] 1.2 Add store port `internal/store/gateways.go` with granular interfaces (GatewayStore, GatewayBindings, GatewayLinks, GatewayOutbox) and fake implementations in `internal/store/fake/gateways.go`
- [x] 1.3 Write migration for `workspace_gateways`, `gateway_user_links`, `gateway_pairing_tokens`, `gateway_chat_bindings`, `gateway_outbox` with the unique constraints from design D12
- [x] 1.4 Implement postgres adapter `internal/store/postgres/gateways.go` (encrypted token ciphertext via the existing secret-encryption pattern; workspace-scoped queries only) + integration tests
- [x] 1.5 Register `tg_dm_`/`tg_group_` binding prefixes in the session-store validators: `tg_dm_` rows join the per-user index under the paired member; `tg_group_` rows are excluded from per-user listings (sched_ precedent) — regression tests for both

## 2. Gateway core

- [x] 2.1 Define `PlatformAdapter` / `InboundHandler` interfaces and normalized `InboundMessage` types in `internal/gateways/` (design D1)
- [x] 2.2 Implement the Telegram adapter (`adapters/telegram`): long-polling runner, webhook receiver payload parsing, send/edit/typing, inline-keyboard approval cards, file download — with a fake transport for tests
- [x] 2.3 Implement the router: DM → member default agent (per-user choice → gateway default), group → bound agent; mint `ExecRequest` with `OriginTelegram`, deterministic session keys, paired member user id, and zero channel coordinates
- [x] 2.4 Implement pairing service: token generation (single-use, 1 h expiry, crypto-random), `/start <token>` consumption, unpair, unpaired-sender refusal with hint
- [x] 2.5 Implement the busy queue: on one-run-per-session guard rejection, enqueue (bounded depth) + ack, resubmit on terminal run outcome (design D4)

## 3. Streaming renderer

- [x] 3.1 Implement GFM→Telegram-HTML conversion with stack-based tag balancing for mid-stream partial content (design D5) + unit tests for unclosed-tag cases
- [x] 3.2 Implement the chunk splitter: 4,000-char budget, paragraph/line/sentence/space boundary preference, code-fence close/reopen across parts + unit tests
- [x] 3.3 Implement the stream controller: placeholder message, 1.2 s debounced edits, 4.5 s typing heartbeat, drain of the agent EventStream (text deltas, tool-call notes), final flush
- [x] 3.4 Implement the parse-failure fallback (single plain-text retry on `can't parse entities`) and rate-limit backoff on 429 Retry-After

## 4. Approvals, ingress, outbox

- [x] 4.1 Implement the approval bridge: detect `approval_required` in the drain, render card, validate callback actor is paired, resume via `Runner.Resume`, record decision on card, refuse new turns while pending
- [x] 4.2 Wire attachment ingress: gateway downloads → existing attachment store/lane classification → multimodal turn construction; size-cap refusals with chat feedback
- [x] 4.3 Implement the STT port with one provider (fail-soft refusal when unconfigured) and voice-note transcription prefixing
- [x] 4.4 Implement the outbox: write-before-send, startup redelivery sweep (attempt budget, 24 h freshness, duplicate-warning prefix, 7-day prune) + tests for the crash-between-generate-and-send case

## 5. Guardrails and lifecycle

- [x] 5.1 Implement the per-chat bot-loop guard (bot-origin consecutive threshold + cooldown) and the gateway circuit breaker (auto-pause on consecutive failures, admin-only resume)
- [x] 5.2 Handle `migrate_to_chat_id`: rewrite binding chat id, log + surface the remap; keep old session keys readable by direct id
- [x] 5.3 Handle `/new` (suffix bump), `/compact`, `/usage`, and `/agent <name>` commands in DMs; `/new` + `/compact` in bound groups
- [x] 5.4 Gateway lifecycle manager: start/stop per enabled gateway on server boot/shutdown and on config changes (transport switch webhook ↔ long-polling)

## 6. API and composition

- [x] 6.1 Handlers `internal/server/handlers/gateways.go`: config CRUD + enable/disable/test, binding create/list/delete, pairing-token generate/revoke, member unpair — admin-gated for config/bindings, member-gated for pairing
- [x] 6.2 Public webhook ingress route with `secret_token` validation; wire gateway manager + handlers into `internal/cli` → router composition root (explicit injection, no nil guards)
- [x] 6.3 Runner integration: `OriginTelegram` constant, origin-tagged events/hook payloads, and the session-index registration rules for `tg_dm_` vs `tg_group_`

## 7. Web UI

- [x] 7.1 Add the Gateways section to settings nav (`/settings/gateways`) and build the pane: bot connect (write-only token), status/username, default-agent picker, transport mode, enable/disable — API-backed, replacing the mock Telegram integrations row
- [x] 7.2 Group bindings list in the pane (bind command display, bound agent, unlink) with binding-conflict toasts
- [x] 7.3 Member pairing modal: copyable `/start <token>` command, live expiry countdown, current linked identity + unlink; read-only gating for non-admins on admin controls
- [x] 7.4 api.ts client methods + store wiring for gateway endpoints; vitest coverage for the pane and modal

## 8. Verification

- [x] 8.1 Fake-based unit tests: router dispatch, pairing expiry/consumption, queue-on-busy, renderer (balancing/splitting/fallback), outbox redelivery, loop guard, circuit breaker
- [x] 8.2 `go build ./... && go vet ./... && go test ./...` green; integration tests for the postgres gateway store green with `TEST_DATABASE_URL`
- [x] 8.3 Extend `scripts/smoke.sh` with gateway API coverage (auth-gated config CRUD, binding conflict 409/422, pairing token lifecycle)
- [ ] 8.4 Manual end-to-end pass against a real bot: pairing → DM turn with streaming → group bind → mention turn → approval card → voice note → restart redelivery; verify web transcript and session-index visibility (DM listed, group not)
