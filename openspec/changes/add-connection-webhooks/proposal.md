## Why

Connections are pull-only: an agent can work GitHub when asked, but nothing happens when a PR opens or an issue gets assigned — the highest-value integration pattern (agents that react to the team's systems) is missing. The gateway pipeline for Telegram/WhatsApp already solved service ingress — signature verification, queueing, routing to a bound agent, approvals — and connections provide the per-service knowledge (recipes) and credential home (HMAC secrets). This change brings service webhooks into the same posture.

## What Changes

- **Recipes declare webhook support**: an event catalog (e.g., `pull_request.opened`, `issues.assigned`), the provider's signature scheme, and a payload→prompt rendering template per event.
- **Per-connection webhook enablement**: enabling webhooks on a connection generates an HMAC secret (encrypted, workspace-bound), exposes the workspace's ingest URL for configuration at the provider, and stores a target binding — one agent plus a thread or channel — that receives the events.
- **Verified event → agent turn**: ingress validates the signature (constant-time), dedupes by delivery id, queues, and renders the event through the recipe's template into a turn for the bound agent. Event content is data, labeled as such — payload text is never treated as instructions.
- **Service-authority runs**: event-triggered runs have no requesting user; they are attributed to a service-authority identity in run metadata and traces, and v1 scopes them to the connection's read-flavored default event set — write-flavored reactions ride the normal tool gates today and the authority-gate change later.
- **Manage UI**: the connection's manage surface gains a webhooks section (enable/disable, ingest URL + secret display-once, target agent picker, event checkboxes) — built only after the ASCII gallery for this surface is approved.
- **GitHub and GitLab webhook recipes live-verified at apply time.**

## Capabilities

### New Capabilities

- `connection-webhooks`: service-event ingress over connections — recipe-declared event catalogs, HMAC-verified ingest with dedupe, target binding to an agent thread/channel, and service-authority run attribution.

### Modified Capabilities

<!-- None: webhook state lives in the new capability's own store surface; connections gain no requirement changes — recipes merely declare webhook data. -->

## Impact

- **Domain** (`internal/domain`): webhook declaration fields on recipes; connection webhook state (enabled, secret envelope, target binding, selected events).
- **Store** (`internal/store`, `internal/store/postgres`): migration — webhook columns on `workspace_connections` + a delivery-dedupe table; fakes updated.
- **New ingress pipeline** (`internal/webhooks` or beside `internal/gateways`): verify → dedupe → queue → route, with a per-service signature verifier and template renderer.
- **Agents/runs**: service-authority attribution in run metadata; no runner behavior change.
- **Server**: public ingest route (workspace + connection scoped).
- **Web**: webhooks section in the connection manage surface (gated on gallery approval).
- **Smoke tests**: signed-delivery → agent-turn coverage with a stub provider.
