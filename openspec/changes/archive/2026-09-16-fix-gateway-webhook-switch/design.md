## Context

The gateway `PUT` contract validates transport and webhook URL as one unit (`ValidateGatewayConfig` requires an `https://` URL for webhook transport and clears any URL under long-polling), but the pane submits them in separate calls: `handleTransport` sends `{transport}` alone; `handleSaveWebhookUrl` sends `{webhook_url}` alone and only renders while transport is already `webhook`. The handler also zeroes the stored URL on every PUT that omits it (`gatewayConfigPayload.WebhookURL` is a plain string with no carry-forward, unlike the `existingTransport`/`existingDefaultAgent`/`existingBotUsername` helpers). Net effect: unconfigured gateways get a raw 422 ("a bot token is required"), and the webhook switch fails with 400 in every configured state. See proposal.md — Why.

## Goals / Non-Goals

**Goals:**
- The Webhook switch works in one user action and one PUT, in every gateway state where it is legal.
- Unconfigured gateways never surface a transport error; the control is absent, not broken.
- The API cannot silently discard a webhook URL it was given.

**Non-Goals:**
- No domain rule changes: webhook transport keeps requiring an `https://` URL (relaxing to "inert webhook" was considered and rejected — it fights the validator and invites half-configured gateways).
- No multi-bot, sidebar, or layout work (separate changes).
- No change to the enable/disable, binding, or pairing flows.

## Decisions

**D1 — UI gating on `configured`.** The Transport card renders only when a token is stored (`gateway.token_hint || gateway.bot_username`), same gate the status card already uses. Alternative (disabled control with tooltip) rejected: it still invites the 422 and needs copy to explain a state the card itself can hide.

**D2 — Combined save for webhook activation.** Clicking *Webhook* in long-polling mode switches the segment locally and reveals the URL input with a **Save & switch** button; the button PUTs `{transport: "webhook", webhook_url}` in one call. If an `https://` URL is already stored, the switch sends both fields together immediately (URL re-sent from `gateway.webhook_url`). Inline validation error from the 400/422 detail renders under the field. Alternative (two-step wizard) rejected as ceremony for two fields.

**D3 — Handler carry-forward of `webhook_url`.** New `existingWebhookURL(existing)` helper joins the `existing*` family: the effective webhook URL is `payload.WebhookURL` when non-empty, else the stored value. This makes the "omit = keep" semantics uniform across token, username, default agent, and URL, and protects any API client (not just this pane) from the silent zero. Alternative (require the field always) rejected — it breaks the same selective-update pattern every other field follows.

**D4 — Switching back to long-polling is immediate.** One click PUTs `{transport: "long_polling"}`; the URL field does not render in long-polling mode (so the discard-on-save path becomes unreachable from the UI), and the stored URL is cleared by existing normalization. The response reflecting "no webhook URL" is the honest state, per spec.

## Risks / Trade-offs

- [Carry-forward re-activates a stale URL when a client switches to webhook after the URL's DNS went stale] → Same exposure as re-sending the stored URL explicitly; the sync failure still surfaces on `status_error` and the admin sees the webhook card. Accepted.
- [Hiding the Transport card on unconfigured gateways removes discoverability of the polling/webhook choice] → The connect card's helper copy already explains ingestion starts on save; transport is a post-connect concern.

## Migration Plan

No schema changes; deploy order unconstrained. Frontend and handler hardening are independently safe (D2/D1 alone fix the UI deadlock; D3 alone fixes the API). Rollback = revert the wave.

## Open Questions

None.
