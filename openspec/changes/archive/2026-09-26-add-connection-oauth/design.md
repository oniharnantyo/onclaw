## Context

Change add-workspace-connections established the connection model: product object materializing an ordinary workspace MCP server, secret token as the server's `Authorization` row through the MCP settings machinery, probe-gated connect, one connection per service, recipes as declared Go data. Recipes already carry an `availability` field, and the Atlassian/Slack/Linear entries ship as `coming_soon` placeholders. OAuth 2.0 (3LO) differs from PAT only in *acquisition* and *lifecycle*: acquisition needs a browser consent round-trip through an instance-registered app; lifecycle needs refresh because providers issue expiring tokens (GitHub App user tokens expire in hours; Atlassian's rotate).

## Goals / Non-Goals

**Goals:**
- Coming-soon cards become working Integrate buttons with zero MCP-runtime changes.
- Refresh and expiry never surprise the user: visible status, one-click recovery.
- Token secrets stay inside the established envelope machinery; no new crypto paths.

**Non-Goals:**
- Per-workspace app registrations (one instance app per provider).
- Proactive background refresh sweeps (refresh-on-resolution with margin only).
- Dynamic client registration or provider-initiated token revocation webhooks.
- Token sharing across connections or workspaces.

## Decisions

- **D1 — Write-through refresh.** A successful refresh re-encrypts the new access token into the materialized server's `Authorization` secret row via the existing MCP settings service; the refresh token, expiry, and granted scopes live in new encrypted columns on the connection row. Alternative rejected: hydrating the `Authorization` header dynamically from the connection at MCP dial time — that threads the connection layer into the MCP connection cache and breaks change 1's D4 (the server row is the single credential home). Consequence: the MCP runtime, cache, and dial path are untouched.
- **D2 — Per-instance app, per-connection tokens.** The instance admin registers one app per provider (self-hosted: their own GitHub/Atlassian app; redirect URI derived from the instance public base URL). Workspaces authorize through it; consented tokens are workspace-scoped rows as usual. Alternative rejected: per-workspace apps — registration burden scales with tenants and duplicates provider review for cloud.
- **D3 — Refresh-on-resolution with margin.** The connections service wraps credential resolution: if the stored expiry is inside the recipe's refresh margin (default 10 minutes), refresh before yielding the token. No background sweeper in v1 — an idle workspace's tokens are allowed to lapse into `expired`, which the gallery surfaces with Reauthorize. This keeps the refresh path on the same code path as every dial and makes failures attributable to one run's log.
- **D4 — Single-use signed state.** The authorize redirect carries an HMAC state parameter binding (workspace, initiating user, one-time nonce) with short TTL; the callback rejects replay, cross-workspace reuse, and unknown attempts before touching the token endpoint.
- **D5 — Probe-gated activation preserved.** The OAuth callback runs the same recipe probe as PAT connect; failure discards the exchanged token set entirely. The store-nothing-on-failure hygiene extends to OAuth: a draft connect attempt leaves no rows.
- **D6 — Atlassian reference recipe; `expired` is a new connection status.** `expired` joins `connected`/`error` as a first-class persisted status so the gallery and agent-attach surfaces can render recovery actions; it is set only by refresh failure and cleared only by reauthorization.

## Risks / Trade-offs

- [Self-hosted instances on private networks cannot receive provider redirects] → the app registration pane shows the derived redirect URI and documents the public-base-URL requirement; localhost/manual URI override is an operator escape hatch documented in the pane, not a product surface.
- [Refresh fails while a run is mid-flight] → the run degrades exactly as it does for an errored MCP server today (tools skipped and marked); the connection shows `expired` for recovery.
- [Provider scope drift (recipes request scopes the provider renames)] → probe failure or consent-time scope mismatch surfaces at connect, not at write time; recipes are release-shippable data.
- [State endpoint is public] → signature + TTL + nonce single-use; callback does no work before validation.

## Migration Plan

1. Migration: token-lifecycle columns on `workspace_connections` (refresh envelope, expires_at, granted scopes) and an instance OAuth apps table; both inert until used.
2. Existing PAT connections are untouched — no backfill, no dual-write.
3. Rollback: down migration; in-flight OAuth connections' refresh state is lost by design (tokens are not exported).

## Open Questions

None blocking. Per-provider specifics (exact scopes per access level, refresh margins, GitHub App vs OAuth App choice for the GitHub recipe) are recipe-data, pinned during apply with live verification like change 1's task 5.3.
