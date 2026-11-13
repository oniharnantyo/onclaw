# Proposal

## Why

The product direction names three inbound trigger surfaces — cron, webhooks, Telegram chat — and two already ship (`scheduler`, `channels`). Webhooks are still an empty seat: the gateway reserves the concept (`internal/gateway/doc.go`), the agent-prompts spec already promises webhook-origin turns autonomous-run treatment, but nothing listens. Third-party systems (GitLab, GitHub, Stripe, CI, automation tools) cannot start agent or group work today, so the gateway's "triggers while you're away" story stops at schedules and Telegram.

## What Changes

- **New webhook trigger surface**: the gateway runs a small HTTP webhook receiver (own listener, gateway-ticker-style attach on Running; default `127.0.0.1:8787`, configurable address; both desktop and server hosts — parity with the scheduler/Telegram pattern, not the server-mode front server).
- **Webhook endpoints**: stored rows with a name, a target (an agent **or** an agent group), a required prompt template, an enabled flag, and credentials. Delivery to an agent target fires a **fresh titled session per delivery** (schedule-shaped; `OriginWebhook`). Delivery to a group target enters the group's shared transcript as an **ordinary user message** via the existing `groups.Send` (mentions route, untargeted routes to the lead; spawned member turns are ordinary user turns — the webhook speaks as the human).
- **Credentials — five verifier engines, preset gallery in the UI**: path token (URL segment), static header equality, raw-body HMAC (GitHub/Shopify/Linear/WooCommerce/Intercom via presets), Standard Webhooks HMAC-SHA256 (GitLab signing token, svix, Clerk, Resend — with `webhook-timestamp` replay window and `webhook-id` retry dedupe), timestamp-composite HMAC (Stripe, Slack, Zoom presets). Endpoints hold any subset of verifiers (≥1); any verifier passing authenticates; a present-but-failed signature fails closed (401). A manual builder exposes the same engines for arbitrary senders. Secrets follow the channels precedent: stored locally in the clear, never returned to the frontend — presence + last-4 hint, shown once at creation, regenerable.
- **Deliveries log**: bounded per-endpoint history (~50) — timestamp, HTTP status, outcome (accepted / auth-failed / paused / too-large / duplicate), the session or transcript-message id it became, the verifier preset that authenticated it (`via`), and the sender's `webhook-id` when present. Duplicate `(endpoint, webhook-id)` returns 200 `duplicate: true` without re-firing.
- **Gateway semantics**: paused/stopped gateway answers 503 + Retry-After (sender retry policy is the queue); unknown path 404; body size cap 413; response is 202 once the turn/message is accepted — fast-respond, like GitLab's receiver guidance.
- **Result surfacing**: agent-target completions enqueue a desktop notification (new Webhooks category, routes to the session); group deliveries surface as transcript rows + deliveries log (no synthetic "group finished" event in v1). Agent-target fires reuse the schedule-fire completion path (`schedule_runs.announceCompletion` pattern).
- **Channels-pane UI**: a Webhooks section with an add-endpoint wizard — basics (name, agent-or-group target, prompt template), preset gallery (GitLab, GitHub, Stripe, Slack, Zoom, Shopify, Linear, Header, Custom), per-preset secret field with a "where to find it" hint, generated URL with copy, per-endpoint **Test** button (backend self-signs a delivery through the real listener, logged as `self-test`), verifier list editor, and the deliveries log.
- **Out of scope (v1)**: asymmetric signatures (Discord Ed25519 / AWS SNS / SendGrid ECDSA), Twilio (URL-signed, form-encoded), Mailgun (signature embedded in body), IP allowlists, mTLS, sync-wait HTTP responses (hold the request until the turn finishes), GitHub `X-Hub-Signature-256` preset rides raw-body HMAC but GitHub event headers stay unexposed in the prompt block, per-endpoint "include headers" config, scheduler targeting groups.

## Capabilities

### New Capabilities

- `webhooks`: the webhook trigger surface — receiver lifecycle and address, endpoint CRUD and targeting (agent fire-session / group transcript ingress), verifier engines and presets with the fail-closed accept rule, deliveries log with dedupe and attribution, gateway pause/stop semantics, the Channels-pane wizard and Test flow.

### Modified Capabilities

- `data-store`: new persistence requirement for webhook endpoints, verifiers, and the bounded deliveries log, and the schema version bump the new tables require (v22 → v23).

## Impact

- **New package** `internal/webhooks` (receiver, verifier engines, endpoint service, delivery dispatch + log); follows the scheduler/Telegram seam pattern — satisfied structurally by the agents manager and groups service, adapted at the composition root.
- **Composition** `internal/cli/app.go`: construct + attach the receiver (gateway ticker pattern), bind the webhook Wails service, shutdown ordering with the bounded-join convention.
- **Origins** `internal/agents/hooks/origin.go`: add `OriginWebhook`; agent-target turns stamp it (hooks/turn-environment blocks already treat webhook origins as autonomous runs per agent-prompts).
- **Store** `internal/store`: migration `000023_webhooks.sql` (schema v22 → v23, additive) for `webhook_endpoints`, `webhook_verifiers`, `webhook_deliveries`, plus the listener-address column on `gateway_settings`; schema-version test pins updated (22 → 23).
- **Notifications** `internal/notifications`: Webhooks category + route resolution for fire sessions.
- **Services/bindings** `internal/services`: webhook endpoint CRUD + deliveries read + Test; regenerate Wails bindings (`task common:generate:bindings`).
- **Frontend** `frontend/src`: Channels-pane Webhooks section (wizard, preset gallery, verifier editor, deliveries log) per the onclaw theming contract, both themes.
- **No change** to `internal/groups` (group ingress is the existing `Send` path, authored as user), `internal/scheduler`, or the server-mode front server (`internal/cli/server_host.go`).
