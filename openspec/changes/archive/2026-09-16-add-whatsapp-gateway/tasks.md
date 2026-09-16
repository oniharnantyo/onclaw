## 1. Domain and schema

- [x] 1.1 Add `GatewayPlatformWhatsApp = "whatsapp"`, `GatewayLaneCloudAPI = "cloud_api"`, `GatewayLaneMultiDevice = "multi_device"` to `internal/domain/gateways.go`; extend `GatewayConfig` with `Lane string` (empty for Telegram) and extend `ValidateGatewayConfig` with lane-scoped rules: WhatsApp+cloud requires webhook transport and a credential envelope; WhatsApp+multi_device requires no token and ignores transport; Telegram ignores lane
- [x] 1.2 Migration `000051_gateway_lane`: add nullable `lane TEXT` to the gateway config table (up/down pair); no backfill — existing rows are Telegram
- [x] 1.3 Add `SessionPrefixGatewayWADM = "wa_dm_"` to the domain session-prefix registry (`internal/domain/agent_session.go`) and thread it through the store's gateway-session filter list; keep `tg_dm_`/`tg_group_` byte-identical
- [x] 1.4 Add `OriginWhatsApp = "whatsapp"` in `internal/gateways/router.go` and make `mentionName` platform-parameterized ("your WhatsApp account" vs "your Telegram account")

## 2. Core generalization (Telegram behavior preserved)

- [x] 2.1 Add `Capabilities()` to `PlatformAdapter` (returns `{CanEdit, CanButton}`); Telegram returns both true; update fakes and all capability-agnostic call sites
- [x] 2.2 Streamer honors `CanEdit=false`: skip the placeholder, send nothing mid-stream, keep the typing heartbeat, deliver the final reply as one outbox message; keep the `CanEdit=true` path byte-identical for Telegram
- [x] 2.3 ApprovalBridge honors capabilities: render the button card only when `CanButton` (button id = `EncodeApprovalCallback` verbatim), else the plain text-reply card; on resolution edit the card when `CanEdit`, else post a receipt follow-up message
- [x] 2.4 Router text-reply approval interception: while an approval card is pending on a `CanButton=false` platform, the next DM text matching APPROVE/DENY case-insensitively synthesizes a `Callback` into the bridge; non-matching text gets the pending-approval notice
- [x] 2.5 Split the render pipeline: `SplitMarkdown` stays neutral; extract the Telegram render/limit constants behind a per-platform flavor (render function + message budget) selected by the service; add the WhatsApp flavor (GFM → `*bold* _italic_ ~strike~ `mono` fenced blocks; lists → `• ` lines; headings → bold; 4096 limit)
- [x] 2.6 Rename the outbox payload field `HTML` → `Body` with a flavor tag; add a startup conversion in the outbox sweep that decodes pending rows in the old shape and re-encodes in the new one (design Migration Plan step 2)
- [x] 2.7 Generalize `Manager.Sync` to iterate the supported platform list and reconcile each platform row (loop over telegram + whatsapp); key the adapter factory dispatch on (platform, lane) in the composition root
- [x] 2.8 Unit tests: capability-matrix fake adapter (CanEdit×CanButton) driving streamer and approval-bridge tests; Telegram-path golden tests assert unchanged behavior; WhatsApp-flavor render tests (degradation cases: lists, headings, split-in-fence)

## 3. Cloud API adapter (`internal/gateways/adapters/whatsappcloud`)

- [x] 3.1 HTTP transport over `graph.facebook.com`: send text/interactive messages, media download (media id → URL → bytes), mark-read+typing call, phone-number health probe; error taxonomy mapping with 131047-class errors marked permanent (design D4)
- [x] 3.2 Update parser: webhook payload structs (batched entries), `messages` → `InboundMessage` (text, image, video, document, audio, sticker; wa id digits normalization), interactive `button_reply` → `Callback` with the bridge encoding, `statuses`/unknown skipped, message-id dedup ring
- [x] 3.3 Adapter wire-up: `Start/Stop` (webhook-mode only, no poll loop), `SendMessage`/`EditMessage` (returns a clear not-supported error used by capability negotiation), `SendTyping` (mark-read + typing flag on the chat's last inbound message id, tracked adapter-side), `SendApprovalCard` (two quick-reply buttons within title limits), `DownloadFile`
- [x] 3.4 Fake transport + adapter unit tests: send parse-fallback parity, dedup, button id round-trip through `DecodeApprovalCallback`, typing call shape

## 4. Webhook ingress and routes

- [x] 4.1 Public `POST/GET /api/v1/webhooks/whatsapp/:ws` handler: GET echoes `hub.challenge` after verify-token match; POST validates `X-Hub-Signature-256` (HMAC-SHA256 of raw body with app secret from the decrypted envelope) before delegating to the manager's webhook receiver
- [x] 4.2 Integration tests: bad/missing signature rejected unauthenticated, challenge echo, batch parse with duplicate message id dropped, status-only payload no-op

## 5. Multi-device adapter (`internal/gateways/adapters/whatsappmd`)

- [x] 5.1 Add `go.mau.fi/whatsmeow` dependency; bridge its sqlstore over the existing `DATABASE_URL` via the pgx stdlib adapter with `whatsmeow_`-namespaced tables keyed per gateway (design D6)
- [x] 5.2 Device lifecycle: connect on Start with reconnect/backoff, surface Connected/Disconnected state, disconnect on Stop, logout teardown; `Capabilities()` returns `CanEdit=true, CanButton=false`
- [x] 5.3 Message wire-up: inbound events → `InboundMessage` (JID localpart digits normalization, media events incl. video→document kind and sticker refusal notice, voice→voice kind), `SendMessage`/`EditMessage` (whatsmeow edit) /`SendTyping` (chat presence) /`SendApprovalCard` (plain instructional message) /`DownloadFile`
- [x] 5.4 Pairing runtime for the admin API: start pairing → QR payload + 8-digit pair code, pollable status (waiting/connected/logged-out), regenerate, logout; state kept on the adapter with manager reconciliation
- [x] 5.5 Unit tests with a fake device client: lifecycle states, normalization, sticker refusal, pairing status transitions

## 6. REST API and composition

- [x] 6.1 Workspace-scoped WhatsApp gateway endpoints under `/api/v1/ws/:ws/gateways/whatsapp`: save per-lane config (labeled fields → one encrypted JSON envelope for cloud; design D5), enable/disable, health/status probe, and the md pairing endpoints (start/status/regenerate/logout); gateways.write permission, field-level 422 validation errors (design D11 lane rules)
- [x] 6.2 Composition root: platform registry with the two factories (whatsapp dispatches on lane), cloud credential decrypt → JSON parse, whatsapp gateway session-index registration with WhatsApp origin indicator (mirror of the Telegram path)
- [x] 6.3 API tests: non-admin refused, lane-scoped validation (missing cloud field, long-polling on cloud rejected), envelope shape never echoed back

## 7. Web UI

- [x] 7.1 `GatewaysPane` multi-platform rework per design D12 gallery: platform tabs (Telegram pane unchanged), WhatsApp pane with lane radio + per-lane config form (cloud labeled write-only fields + webhook URL/verify-token copy rows; md ban-risk notice + QR/pair-code + live status + logout), shared default-agent picker and enable/disable
- [x] 7.2 Member pairing flow per platform: `/start <token>` with expiry countdown, linked-identity display and unlink for the signed-in member
- [x] 7.3 Guard-rejection toasts (invalid credentials, lane validation, non-admin) and non-admin read-only rendering
- [x] 7.4 Web tests: pane renders per state (unconfigured, cloud connected, md pairing, md connected), API-layer gateways client for the new endpoints

## 8. Spikes and verification

- [ ] 8.1 Spike: live cloud typing-indicator behavior (payload shape, persistence, rate behavior on a test number) → pin the heartbeat constant; abort-criteria: if mark-read+typing is unavailable on test numbers, document and fall back to no typing (cloud lane stays single-final-message)
- [ ] 8.2 Spike: verify button reply id survives ~34-char payload round-trip on a test number (design risk: undocumented id limit)
- [x] 8.3 Verify `go build ./...`, `go vet ./...`, `go test ./...`, and the integration suite pass with both adapters registered

## 9. End-to-end and docs

- [x] 9.1 Smoke coverage: WhatsApp cloud webhook signature rejection, challenge echo, message→run→outbox delivery via a stubbed Meta endpoint; md pairing status transition via the fake device
- [x] 9.2 README/docs: WhatsApp setup guide (cloud: Meta app, phone number, dashboard webhook paste; md: QR flow and ban-risk statement)
- [x] 9.3 Update `scripts/smoke.sh` numbering and helpers for the new surface
- [ ] 9.4 Manual pass: pair a real WhatsApp account on the md lane and a test number on the cloud lane; exercise DM turns, `/new`, approvals (button + text-reply), voice note, lane switch preserving the session
