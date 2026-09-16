## Why

The gateway core is platform-neutral by design but ships only Telegram (`adapters/telegram`), and six core seams are Telegram-flavored (lifecycle hardcodes the platform, render pipeline emits Telegram HTML, streamer assumes editable messages, session keys carry a `tg_` prefix with no platform in the key). WhatsApp is the highest-value second surface for a self-hosted agent workspace — and it genuinely differs from Telegram: no message editing, a 24-hour customer-service window, webhook-only ingestion, no groups in the official API, and no native buttons on consumer-protocol lanes. This change generalizes the core and adds WhatsApp as a first-class platform in two lanes: the official Cloud API for production use and a whatsmeow (multi-device protocol) lane for self-hosters who accept ban risk — mirroring the peer landscape (OpenClaw ships Baileys built-in; community plugins add the Cloud API).

## What Changes

- New WhatsApp platform with two selectable lanes per workspace gateway: `cloud_api` (official Meta Cloud API, webhook-only) and `multi_device` (whatsmeow in-process device client, QR/pair-code setup, no webhook needed).
- Core generalization (behavior-preserving for Telegram): platform registry in the lifecycle manager (iterate supported platforms, per-platform adapter factory), adapter capability negotiation (`CanEdit`, button support) so the streamer and approval bridge adapt per platform, platform-flavored render pipeline (WhatsApp markdown subset alongside Telegram HTML), platform-neutral outbox payload body, per-platform session-key prefixes (`wa_dm_` beside `tg_dm_`), and per-platform run origin + mention copy.
- WhatsApp session keys normalize identity to bare phone digits (cloud `wa_id`, whatsmeow JID localpart) so switching lanes preserves sessions, bindings, and paired links.
- Streaming per lane: cloud lane shows the typing indicator (mark-as-read + typing) and delivers one final message (no edits, no placeholder); the multi-device lane reuses the existing placeholder + debounced-edit streaming via `CanEdit`.
- Approvals per lane: cloud lane uses interactive quick-reply buttons (button reply id carries the existing platform-neutral callback encoding) with a receipt follow-up message on resolution; the multi-device lane has no usable buttons (WhatsApp deprecated them on consumer accounts) and uses a text-reply flow — while a card is pending, the next DM `APPROVE`/`DENY` is intercepted as the decision.
- 24-hour window policy (cloud lane): free-form replies that fail with the window-expired error (131047) mark the outbox entry dead and surface observably (gateway health/logs); no template-based re-engagement.
- Cloud-lane webhook ingress: Meta `X-Hub-Signature-256` HMAC validation, `GET hub.challenge` verification handshake, batched entry parsing, message-status events ignored; webhook registration remains a manual step in the Meta dashboard (the settings UI shows the URL and verify token to paste).
- New `whatsapp` dependency: `go.mau.fi/whatsmeow` (active, MPL-2.0) for the multi-device lane, with its device-session state persisted via its Postgres sqlstore bridged over the existing database URL.
- Gateways settings pane becomes multi-platform: Telegram section unchanged, WhatsApp section with lane choice, structured credential fields (cloud) or QR/pair-code pairing flow with ban-risk notice (multi-device), default-agent picker, enable/disable, and the member pairing flow.
- Scope guards: DM sessions only in v1 (no WhatsApp groups — impossible on cloud, deferred on multi-device), no outbound media, no template management, no horizontal scaling of the device lane.

## Capabilities

### New Capabilities
- `whatsapp-gateway`: WhatsApp gateway as a two-lane platform — configuration and lane selection, member pairing, DM routing, digits-normalized session keys, per-lane streaming and approvals, attachment/voice ingress, outbox delivery with the 24-hour window policy, webhook ingress with Meta signature validation, guardrails, admin API, and private session-index registration.

### Modified Capabilities
- `web-app/settings`: the Gateways pane requirement extends from Telegram-only to multi-platform — it adds the WhatsApp section (lane selection, cloud credential form, multi-device QR/pair-code pairing with connection status and ban-risk notice) alongside the unchanged Telegram surface.

## Impact

- **Backend:** `internal/gateways` (lifecycle, streamer, approvals, render, router, outbox payload, types — capability negotiation and platform registry), new `internal/gateways/adapters/whatsappcloud` and `internal/gateways/adapters/whatsappmd` packages, `internal/domain` (gateway lane value, `wa_dm_` prefix, WhatsApp origin), `internal/server` (webhook route, gateway REST routes, runtime factory dispatch and per-platform probe), migration adding the `lane` column to gateway config (next in sequence after 000050).
- **Frontend:** `GatewaysPane` multi-platform rework; no other web surfaces change.
- **Dependencies:** add `go.mau.fi/whatsmeow` (multi-device lane only; MPL-2.0). Cloud lane is raw REST, no SDK.
- **Tracing:** WhatsApp runs export under the existing gateway origin tag in Langfuse; dead window-expired deliveries surface in gateway health and logs.
- **Schema:** one migration (gateway config `lane` column) plus whatsmeow-owned device-session tables in a separate namespace.
