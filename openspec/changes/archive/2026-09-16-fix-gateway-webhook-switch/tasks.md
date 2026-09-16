## 1. Backend hardening

- [x] 1.1 Add `existingWebhookURL` carry-forward in `PutConfig` (handlers/gateways.go): effective URL = payload value when non-empty, else stored; add handler tests — omitted URL keeps stored URL, webhook activation without any URL refused naming `webhook_url`, tokenless transport change refused naming `token`, long-polling switch clears URL and response reflects it
- [x] 1.2 Verify `ValidateGatewayConfig` normalization against the new contract with a domain test table (webhook+https OK, webhook+empty refused, long-polling clears) — no rule edits expected

## 2. Pane: gating and combined save

- [x] 2.1 Gate the Transport card on `configured` (token_hint or bot_username present); unconfigured pane renders connect + pairing only, no transport control
- [x] 2.2 Rework `handleTransport` for webhook: reveal inline URL input (prefilled from `gateway.webhook_url`) with a **Save & switch** button that PUTs `{transport: "webhook", webhook_url}` together; when an https URL is already stored, the switch PUTs both fields immediately; surface fielded 400/422 detail inline under the input
- [x] 2.3 Keep long-polling switch as the immediate one-click save; remove the URL input and `handleSaveWebhookUrl` from long-polling render so the discard path is unreachable
- [x] 2.4 Component tests: unconfigured pane hides transport; webhook click in long-polling reveals input and sends combined payload; stored-URL switch sends both fields; validation error renders under input

## 3. End-to-end verification

- [x] 3.1 smoke.sh: extend the gateway section — unconfigured transport PUT still 422 naming `token`; connect token; switch to webhook with URL in one PUT succeeds; transport-only PUT with stored URL keeps it; switch back to long-polling clears URL
- [x] 3.2 Manual pass: fresh workspace → click Webhook before connecting (no 422 surface, card absent) → connect bot → Save & switch with a public URL → confirm Telegram webhook registered → switch back to long-polling
