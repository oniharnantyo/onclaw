## Why

Switching the Telegram gateway to webhook transport is impossible through the UI in every gateway state, and clicking the *Webhook* segment on an unconfigured gateway surfaces a 422 ("a bot token is required") that reads as a system error rather than a step the user skipped. The transport switch and webhook URL are validated server-side as one unit, but the UI submits them separately — a deadlock the pane must stop fighting.

## What Changes

- Gate the Transport card on a configured gateway (token saved): hidden — not merely disabled — until a bot token exists, eliminating the unconfigured 422 path.
- Make the Webhook switch a combined save: clicking *Webhook* reveals the URL field inline and one `PUT` carries `{transport: "webhook", webhook_url}` together (the payload already accepts both fields; no wire change).
- Switching back to *Long-polling* stays a one-click immediate save (`{transport: "long_polling"}`); the stored webhook URL is cleared by existing normalization, so re-enabling webhook later re-asks for the URL.
- Backend hardening: `PutConfig` carries forward the stored `webhook_url` when the payload omits it (mirroring the existing `existingTransport`/`existingDefaultAgent` helpers), so a transport-only PUT can no longer zero a stored URL under any client.
- Fix the silent-clear lie: the URL field no longer renders in long-polling mode, and the save path that would be discarded is unreachable.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `telegram-gateway`: Transport switching requirements — webhook activation SHALL require both transport and a valid `https://` webhook URL in one atomic save; a transport change without a token SHALL be refused with a fielded validation error naming `token`; an omitted `webhook_url` SHALL carry the stored URL forward instead of clearing it.

## Impact

- `web/src/screens/settings/GatewaysPane.tsx`: transport card gating, combined-save handler, inline URL reveal on switch.
- `internal/server/handlers/gateways.go`: `PutConfig` webhook-URL carry-forward (one helper in the `existing*` family).
- `internal/domain/gateways.go`: no rule changes — `ValidateGatewayConfig` keeps requiring `https://` for webhook transport.
- Tests: handler PUT tests (carry-forward, refusal paths), pane component tests (gating, combined save).
