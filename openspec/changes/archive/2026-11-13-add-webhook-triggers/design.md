# Design

## Context

Two trigger surfaces exist and set the house pattern: the scheduler (gateway-ticker attach, a narrow `Runs` seam, fire-sessions 1:1 with runs, completion announce + notification) and the Telegram channel (owns its networking, narrow `Runtime` seam, origin-stamped turns, write-only credentials). The composition root is `internal/cli/app.go`; desktop mode has no inbound HTTP listener today (server mode's front server is behind the `server` build tag and is the UI surface, not a trigger surface). Webhooks are pre-named as a trigger adapter (`internal/gateway/doc.go`) and as an autonomous-run origin (`openspec/specs/agent-prompts/spec.md`). See proposal.md for why.

## Goals / Non-Goals

Goals: a webhook receiver that behaves identically on both hosts; endpoints targeting agents (fresh fire sessions) or groups (transcript ingress); sender-auth that covers GitLab and the common HMAC families via presets plus a manual builder; a deliveries log that makes silent failures diagnosable; secrets handled exactly like Telegram's token.

Non-Goals: asymmetric signature schemes (Discord/SNS/SendGrid), Twilio and Mailgun schemes, IP allowlists, mTLS, sync-wait responses, scheduler→group targeting, payload templating beyond the fixed body block, per-endpoint header inclusion.

## Decisions

**D1 — The receiver is its own listener, wired at the composition root.** An `internal/webhooks` package owns an `net/http` server attached to the gateway the way the scheduler's ticker attaches (start on first Running, bounded teardown on Stop/app close). Not a route on server mode's front server: desktop has no listener there, the auth domains differ (bearer vs per-endpoint verifiers), and the parity rule wants identical behavior on both hosts. Alternative rejected: piggybacking the Wails asset handler — it does not exist in desktop mode.

**D2 — Agent targets are schedule-shaped; group targets speak as the user.** Agent deliveries mint one fresh titled session via a `Runs`-style seam (mirror of `scheduleRuns`: CreateSession → SetTitle → Submit with `hooks.OriginWebhook`), reusing the turn-wait/excerpt/notification announce pattern from `internal/cli/schedule_runs.go` as a webhook-side adapter rather than generalizing the scheduler's. Group deliveries call the existing `groups.Service.Send` with `SendRequest{Text: rendered}` — zero groups-code changes, full routing/mention/task behavior. The user chose total masquerade: no author-kind extension, no `SendRequest` origin field; provenance lives in the delivery log (which records the transcript message id). Alternatives rejected: new group author kind (rejected by user), persistent per-endpoint agent sessions (surprising cross-event context).

**D3 — Verifier engines are code; presets are config.** Five engines (`path`, `header`, `raw-hmac`, `stdweb`, `ts-hmac`) each verify in constant time; presets (GitLab, GitHub, Stripe, Slack, Zoom, Shopify, Linear, Header) are named configs that pre-seed an endpoint's verifier list and render as one-secret-field cards in the wizard. Accept rule: any verifier pass authenticates; a request carrying one of the endpoint's expected signature headers with a bad signature answers 401 with no fall-through (signature presence = intent — GitLab's own migration guidance). Raw-body HMAC must run on the exact bytes the sender signed: the receiver reads the raw body (size cap applies), verifies, then parses. Stripe's secret handling (`whsec_` decoding) and Slack/Zoom's `v0:{ts}:{body}` composition are pinned against the senders' official verification docs in tasks. Alternative rejected: only Standard Webhooks (leaves GitHub/Shopify/Slack senders unable to authenticate).

**D4 — Replay dedupe rides the sender's own delivery id.** Standard Webhooks guarantees `webhook-id` stability across sender retries, so a repeat `(endpoint, webhook-id)` within log retention answers 200 duplicate without firing. Alternatives rejected: own idempotency-key scheme (invented surface), no dedupe (double sessions after every sender retry).

**D5 — HTTP semantics mirror sender expectations.** 202 once the turn/message is accepted (fast-respond, GitLab receiver guidance); 503 + Retry-After while paused (sender retry is the queue — an HTTP request cannot be held like a poll loop); 404 unknown path; 401 auth; 413 over cap; 4xx when the target no longer resolves (misconfiguration, tell the sender to stop). Mirrors the scheduler's miss/failed recording, but the wire answer replaces the persisted slot.

**D6 — Secrets follow the channel-credential precedent.** Stored in the clear locally (single-user app), never returned to any frontend read — presence plus a short last-4-style hint; blank save keeps; regenerate re-issues; shown once at creation. Applies uniformly to every engine's secret.

**D7 — Schema v23 (migration `000023_webhooks.sql`): three tables, not a JSON blob.** `webhook_endpoints` (id, unique name, target agent-id/group-id, template, enabled), `webhook_verifiers` (ordered child rows: kind, preset label, non-secret config, secret columns), `webhook_deliveries` (per-endpoint bounded log: timestamp, status, outcome, session/message id, verifier reference, sender delivery id). The child table over a JSON column because the delivery log must name the verifier that authenticated, and per-verifier secret handling (write-only, blank-keeps) needs column-level care. Listener address rides the gateway's singleton trigger-config row (`gateway_settings.webhook_addr`; loopback:8787 default). Schema-version test pins (22 → 23) update with the migration.

**D8 — Test is a self-signed loopback delivery.** The backend composes a payload, signs it with the endpoint's own credentials, and POSTs through the real listener; logged `via: self-test`. Proves bind, port, verifier, and dispatch end-to-end — and matches the re-enable/health idiom senders like GitLab train users to expect.

**D9 — Wails surface.** One `webhook` service bound in both hosts (CRUD, deliveries read, test, listener-state read), bindings regenerated via `task common:generate:bindings`; `transport.ts` stays hand-maintained per house convention.

## Risks / Trade-offs

- [Non-loopback bind exposes live endpoints] → ≥1 verifier required at save, loopback default, UI warning on non-loopback binds.
- [HMAC byte-exactness differs per sender — Stripe decodes its `whsec_` secret, GitHub prefixes `sha256=`, Slack hashes `v0:{ts}:{body}`] → one pinned verification vector per preset in the engine tests, taken from the senders' official docs.
- [Timing side-channels on token compare] → `hmac.Equal`/`subtle` for every secret comparison including the path token.
- [Burst deliveries to one group] → `Send`'s per-group write lock and lane queueing already serialize; the receiver answers 202 after acceptance, never inline.
- [Port 8787 already held] → bind failure surfaces as a receiver error state in the UI (browser-use lockfile precedent), config editable without restart.
- [Masquerade complicates auditing "who did this"] → delivery log records message/session ids and `via`; accepted as the user's explicit choice.
- [Schema pins] → the schema-version test pins (22 → 23) update in the same change as the migration.

## Migration Plan

Additive migration to v23 (three new tables + the `gateway_settings.webhook_addr` column); no data rewrite, existing rows untouched. Rollback is the house convention (restore from backup); disabling all endpoints stops all ingress without a downgrade.

## Open Questions

- Whether Test stays available on disabled endpoints (convenient for setup vs surprising side effect) — decidable at implementation without spec impact.
- Default port confirmation (8787 chosen for disambiguation from server mode's 8080 and common dev ranges) — a config default, trivially changeable.
