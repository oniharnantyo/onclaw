## Context

The gateway core (`internal/gateways`) is platform-neutral in name but Telegram-shaped in six seams: the lifecycle manager loads only the Telegram platform row, the render pipeline emits Telegram HTML, the streamer assumes editable messages, the approval bridge edits cards on resolution, session keys use `tg_` prefixes with no platform in the key, and the composition root has a Telegram-only adapter factory plus `getMe` health probe. The store, domain structs, and schema were explicitly designed for more platforms ("the next adapter is a data change, not a schema one"). This design generalizes those seams behavior-preserving for Telegram and adds two WhatsApp adapters behind them.

Verified platform facts that shape the design: the Cloud API has no edit-sent-message endpoint; typing indicators exist but are tied to mark-as-read of the user's message; ingestion is webhook-only with `X-Hub-Signature-256` HMAC validation and a `GET hub.challenge` verification handshake; webhook registration with Meta is manual (no `setWebhook` equivalent); quick-reply buttons cap at three with ~20-char titles; free-form sends outside the 24-hour customer-service window fail with error 131047; text bodies cap at 4096 chars with a markdown-subset formatter (no lists, no headings). On the multi-device lane (whatsmeow): message editing and chat-presence typing work, but native buttons are dead — WhatsApp deprecated them for consumer accounts and whatsmeow's interactive rendering is unreliable.

## Goals / Non-Goals

**Goals:**
- A workspace runs WhatsApp through exactly one lane at a time (`cloud_api` or `multi_device`) on the same one-row-per-(workspace, platform) model as Telegram.
- Telegram behavior is byte-for-byte preserved at every generalized seam; its spec is untouched.
- Lane switching preserves sessions, bindings, and paired links (digits-normalized wa id).
- The new core seams are written once and generically enough that a future platform adapter (Slack, Discord) touches no core code beyond a factory registration.

**Non-Goals:**
- WhatsApp groups (impossible on the cloud lane; deferred on the multi-device lane).
- Template management and template-based re-engagement outside the 24-hour window.
- Outbound media (text-only replies, same as Telegram v1).
- Horizontal scaling of the multi-device lane (single device connection, single instance).
- whatsmeow button/interactive attempts of any kind.

## Decisions

### D1 — Two lanes, discriminated by an explicit `lane` column
`gateway_configs` gains `lane TEXT NULL` (migration 000051). Telegram rows keep `lane` NULL and are unaffected; WhatsApp rows carry `cloud_api` or `multi_device`. Lane is never inferred from credential shape — an explicit column keeps validation, reconciliation, and the settings UI honest.
*Alternative rejected:* inferring lane from envelope contents (fragile, unvalidatable) or two platform values `whatsapp`/`whatsapp_md` (breaks the one-platform-one-row semantics and doubles specs).

### D2 — Adapter capability negotiation replaces assumed Telegram features
`PlatformAdapter` gains a `Capabilities()` method returning `{CanEdit bool, CanButton bool}` (typing stays universal: every adapter implements it or no-ops). The streamer consults it: `CanEdit=false` skips the placeholder entirely, sends nothing mid-stream, heartbeats typing, and delivers the final reply as one outbox message. The approval bridge consults it for card resolution: edit the card when `CanEdit`, otherwise send a receipt follow-up; render buttons when `CanButton`, otherwise the text-reply card.
*Alternative rejected:* no-op `EditMessage` implementations (dishonest contract — callers cannot distinguish "updated" from "silently dropped").

### D3 — Per-lane approval mechanics
Cloud lane: interactive message with two quick-reply buttons; the button reply id carries `EncodeApprovalCallback(interruptID, decided)` verbatim (the existing platform-neutral encoding, ~34 chars — well within observed id limits; task 8.x probe-verifies). Resolution posts a receipt follow-up because the card cannot be edited. Multi-device lane: plain-message card; while pending, the router intercepts the session's next DM text — case-insensitive `APPROVE`/`DENY` synthesizes a `Callback` into the existing bridge, anything else gets the pending notice. The interception lives in the router (message-classification layer), not the bridge, so the bridge's callback contract stays untouched.
*Alternative rejected:* number replies ("1"/"2") — words survive screenshot/translation better and read naturally in the transcript.

### D4 — 24-hour window: dead-letter, never re-engage
The cloud adapter classifies error 131047 (and its subcode family) as permanent: the outbox marks the entry dead and the failure surfaces in gateway health and logs (Langfuse already carries the turn's trace; delivery death is logged against the gateway, not force-joined to a closed trace). No template sending, ever, in this change.

### D5 — Credentials per lane
Cloud lane: one encrypted envelope (existing AES-256-GCM secrets seam, unchanged) whose plaintext is a JSON object `{access_token, phone_number_id, app_secret, verify_token}`. The decryptor contract stays string-shaped; the adapter parses. The settings form is four labeled write-only fields — the JSON packing is storage detail, never UI. Multi-device lane: no token at all — the device session lives in whatsmeow's own store; the gateway stores only `lane`, display name, and enable state.
*Alternative rejected:* four new encrypted columns (schema churn, four decrypt calls, no benefit over one opaque envelope).

### D6 — whatsmeow session storage bridged over the existing database URL
whatsmeow's sqlstore speaks `database/sql`; the composition root opens a stdlib handle via the pgx stdlib adapter on the same `DATABASE_URL` and lets whatsmeow own its tables in a `whatsmeow_` namespace (one container table per workspace gateway, keyed by gateway id). The main migration chain does not manage these tables.
*Alternative rejected:* SQLite file per gateway (second storage engine, backup story divergence) or reimplementing the store over pgx (upstream churn we'd inherit).

### D7 — Digits-normalized session keys
`GatewayDMSessionKey` prefixes become platform-derived: `tg_dm_` for Telegram (unchanged), `wa_dm_` for WhatsApp, drawn from a domain prefix map that the store's gateway-session filter already consumes. The WhatsApp adapter normalizes identity to bare phone digits — cloud `wa_id` is digits already; whatsmeow JID takes the localpart before `@s.whatsapp.net` (groups would be `@g.us`, excluded since DM-only). Distinct prefixes also kill the latent cross-platform collision (Telegram ids and wa ids are both digit strings).
*Alternative rejected:* unified `gw_<platform>_dm_` scheme (requires backfilling live Telegram session rows for naming symmetry alone).

### D8 — Cloud webhook ingress: validate, verify, dedup, ignore statuses
New public route `POST/GET /api/v1/webhooks/whatsapp/:ws`. GET answers the Meta verification handshake (`hub.mode=subscribe`, `hub.verify_token`, echoes `hub.challenge`); POST validates `X-Hub-Signature-256` (HMAC-SHA256 of the raw body with the app secret from the decrypted envelope) before parsing, then walks batched entries: `messages` parse into `InboundMessage` (text, image, video, document, audio, sticker, interactive button_reply → `Callback`), `statuses` and everything else are skipped. Redelivered message ids drop through the existing dedup-ring pattern (keyed on platform message id). Registration stays manual in the Meta dashboard; the settings pane shows the callback URL and verify token to paste. The multi-device lane never touches this route.

### D9 — WhatsApp render flavor
New renderer alongside the Telegram one: GFM markdown → WhatsApp markdown subset (`*bold*`, `_italic_`, `~strike~`, `` `mono` ``, fenced blocks), with degradations: list items become `• `-prefixed lines, headings become bold lines, links stay inline (no preview toggle exists — `DisablePreview` is ignored by this adapter). The 4096-char limit reuses the same chunk budget/split machinery (`SplitMarkdown` stays platform-neutral; only the render step and limit constants differ). The outbox payload's `HTML` field becomes `Body` with a render-flavor tag — pending rows from before the rename are drained at deploy (see Migration Plan).

### D10 — Platform registry in composition and lifecycle
`Manager.Sync` iterates a platform list (telegram, whatsapp) and reconciles each row independently — the hardcoded `GatewayPlatformTelegram` lookups become loops. The composition root's adapter factory dispatches on (platform, lane); the health probe per lane: cloud = `GET /{phone_number_id}` metadata call; multi-device = device `IsConnected()` state surfaced through the pairing-status endpoints. Run origin gains `OriginWhatsApp` (`router.go`), and mention copy becomes platform-parameterized ("your WhatsApp account").

### D11 — Small locks folded from the explore
Blue-tick side effect of cloud typing is accepted (spec'd). Webhook status events are ignored. Video → document lane; stickers refused with a notice. Commands work as plain leading-slash text (no `@botname` suffixes exist on WhatsApp; the existing suffix-strip is harmless).

### D12 — Settings pane gallery (design gate)

```
┌─ Settings → Gateways ────────────────────────────────────────────────┐
│ [ Telegram ]  [ WhatsApp ]                     ← platform tabs       │
│                                                                      │
│ WhatsApp pane, idle (never configured):                              │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │ WhatsApp gateway            [ ● Not configured ]      [ Enable ] │ │
│ │ Lane   (•) Official Cloud API   ( ) Multi-device (personal)      │ │
│ │        ── info text under each option, per lane ──               │ │
│ │ Default agent for DMs   [ Atlas                       ▾ ]        │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│ WhatsApp pane, cloud lane form:                                      │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │ Access token        [ •••••••••••••••••• ]  (write-only)          │ │
│ │ Phone number ID     [ 123456789012345 ]                          │ │
│ │ App secret          [ •••••••••••••••••• ]  (write-only)          │ │
│ │ Verify token        [ •••••••••••••••••• ]  (write-only)          │ │
│ │ Webhook (paste in Meta dashboard):                                │ │
│ │   https://host/api/v1/webhooks/whatsapp/acme   [Copy]             │ │
│ │   Verify token shown beside                       [Copy]          │ │
│ │                                             [ Save & connect ]    │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│ WhatsApp pane, multi-device pairing:                                 │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │ ⚠ Uses an unofficial protocol. Meta may ban the account.          │ │
│ │   Recommended for personal/test numbers only.                     │ │
│ │ ┌─────────┐   Pair code: 4821-9376                                │ │
│ │ │   QR    │   Settings → Linked devices → Link a device           │ │
│ │ │  code   │   [ Regenerate ]           Status: ● Waiting for scan │ │
│ │ └─────────┘   (status settles to ● Connected after scan)          │ │
│ │   [ Log out device ]   (when connected)                           │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│ Member pairing footer (both platforms, per signed-in member):        │
│   Link your account:  /start a3Zk…9Qm2   [Copy]   expires in 14:52   │
│   Linked as @oni (Telegram) · +62••• (WhatsApp)        [ Unlink ]    │
└──────────────────────────────────────────────────────────────────────┘
```

## Risks / Trade-offs

- [whatsmeow is reverse-engineered; Meta can break it or ban accounts] → the lane ships behind an explicit ban-risk notice, is never the default selection, and the cloud lane needs zero whatsmeow code (build tag keeps the dependency out of cloud-only deployments if desired later).
- [Typing heartbeat cadence unknown until probed (indicator persistence varies)] → task spikes the real payload and tunes the interval; worst case the indicator flickers, never blocks delivery.
- [Button reply id limits undocumented] → payload is ~34 chars; the adapter probe test fails loudly if Meta truncates, and the fallback is a short server-side id map in the card state.
- [Meta webhook retries may not be idempotent across restarts (in-memory dedup ring)] → same exposure as Telegram today; ring covers the retry window (seconds-minutes), and idempotency across restarts is already the outbox/queue's job for sends, with run-dedup via session queueing for ingress.
- [`go.mau.fi/whatsmeow` dependency tree size] → confined to the md adapter package; cloud-only builds unaffected at runtime.
- [Pending outbox rows from before the payload-field rename] → deployment drains or converts pending rows in the startup sweep before the new reader runs (task 9.x).
- [Two lanes double the approval/streaming test matrix] → capability negotiation funnels both lanes through the same streamer/bridge tests with a fake capability-matrix adapter.

## Migration Plan

1. Deploy order: migration 000051 (`lane` column, nullable, no backfill) → generalized core (Telegram untouched behavior) → cloud adapter → md adapter. Each step leaves the server runnable; later steps simply add platforms.
2. The outbox payload rename (`HTML` → `Body` + flavor tag) lands with a startup conversion for pending rows: decode-as-old, re-encode-as-new, in the same transaction as the sweep claim.
3. Rollback: the lane column is additive and nullable; reverting application code leaves it unused. whatsmeow-owned tables are dropped only on explicit md-lane teardown (logout), never by down-migrations.

## Open Questions

- Exact cloud typing-indicator heartbeat interval and whether repeated mark-read calls rate-limit on test numbers (spike in task 8.1; interval is a constant).
- Whether Meta's phone-number metadata endpoint is the cheapest cloud health probe vs. a balance-less credential check (settled in task 6.1; either satisfies the spec's "validates reachability").
