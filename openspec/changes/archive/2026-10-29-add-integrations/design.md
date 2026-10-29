# Design

## Context

The mechanism layer already exists: `internal/agents/mcp` implements stdio/HTTP/SSE transports, probe, OAuth 2.1 (browser flow + PKCE + loopback redirect + dynamic client registration + `WWW-Authenticate` protected-resource discovery), token persistence and headless refresh; `MCPServerAuth` (persisted as JSON in `mcp_servers.auth`) already carries `ClientID`/`ClientSecret`/`Issuer` fields that are currently only populated for DCR-registered clients. The frontend already has the two patterns this change composes: the provider type catalog (`catalog.ts`, with the "requires base URL" template precedent) and the `BrandMark` offline glyph map. The research backing the catalog is in the exploration record: 20+ first-party remote MCP servers verified 2026-10-27, with per-service auth facts (which methods each endpoint accepts, header styles, setup prerequisites).

## Goals / Non-Goals

**Goals:**

- Service-centric guided connect that produces ordinary `MCPServer` rows — zero new runtime tool machinery.
- Catalog as data: adding a service is a frontend catalog entry, not code, whenever its auth fits an existing method kind.
- Credential secrecy at parity with providers, closing the current raw-header round-trip exposure for anything entered through Integrations.
- Unlock the no-DCR vendors (Google, Slack, Asana, Box, Dropbox) via the pre-registered client path.

**Non-Goals:**

- A credential vault or any non-MCP credential consumer (nothing spends such tokens today).
- Changing the MCP pane's raw editing, per-agent allowlist semantics, or tool discovery.
- Catalog curation beyond the launch set; Tier-3 services (Vercel allowlist, ClickUp caps) land as data later, if ever.

## Decisions

- **D1 — An integration is a tagged `MCPServer` row, not a new table.** Add a nullable `service` column (SQLite migration) plus integration credential storage inside the `mcp_servers.auth` JSON document (new fields alongside the existing `MCPServerAuth` shape). Delete-integration = delete-server; probe/authorize/refresh/allowlists are inherited. Alternative considered: a separate `integrations` table with linked server ids — rejected because it duplicates state and needs a sync story for zero non-MCP consumers. A row without a tag behaves exactly as today.

- **D2 — The catalog lives in the frontend; the backend stores an opaque tag.** Like `PROVIDER_CATALOG`, the catalog module owns labels, icons, categories, keywords, endpoints, auth-method declarations, and setup hints; `service` on the row is a non-empty string the backend does not interpret. Alternative: a Go-side known-services enum that rejects unknown tags — rejected as ceremony with no consumer (nothing backend reads the tag except to expose it). Drift risk is bounded because the tag is write-once per row from the frontend catalog.

- **D3 — Auth methods are catalog-declared kinds, mapped onto existing machinery.**
  - `oauth_dcr` → today's flow unchanged (GitLab, GitHub, Atlassian, Linear, Notion, Sentry, …).
  - `oauth_confidential` → authorize skips DCR when `MCPServerAuth.ClientID` is set and uses the stored secret; same browser/PKCE/loopback flow (Google, Slack, Asana, Box, Dropbox). The store fields already exist; the change is confined to the authorize path.
  - `token` with a per-service header style: `bearer`, `token=<k>` (PagerDuty), `basic email:key` (Atlassian admin-gated), custom scheme (`Sentry-Bearer`), custom header name (`X-Goog-Api-Key`). The connection layer renders the styled header from the stored credential at dial time.
  - Authorize URL extra params (Google's `access_type=offline&prompt=consent`) are catalog-declared per entry and appended by the authorize path when present.

- **D4 — Integration credentials live outside the header map and never round-trip.** Token credentials and client secrets ride in the `auth` JSON document (which the settings service redacts), not in `headers` (which round-trips verbatim today). Reads/listings of integration rows expose `CredentialSet` + last-4 hint, mirroring provider secrecy; blank-update-keeps-stored. The raw MCP pane hides the redacted fields from its header editor. Alternative: redact specific `headers` keys — rejected as fragile (which keys? what about non-secret custom headers users legitimately set?).

- **D5 — Pane = filter rail over a flat icon grid.** Category chips (All + declared categories, catalog-wide counts) and a keyword search compose; no section headers. Tiles reuse the `BrandMark` grammar via an extended glyph map keyed by service id, initial-glyph fallback for unknown ids. Status dot reuses the MCP row grammar (connected/failed/authorization-required/neutral). Alternatives considered: category-grouped sections (user rejected), vendor grouping (rejected — Gmail is Communication, Calendar is Productivity).

- **D6 — Google Workspace is a family entry.** One sheet: shared inputs (client ID/secret for the user's flagged Google Cloud OAuth client) plus a product checklist; completing it creates one tagged row per checked product, each probed independently. The family's shared client is stored per product row (duplication of the secret across rows is acceptable at this scale and keeps rows self-contained). Loopback redirect works unchanged: Google allows `http://127.0.0.1` with any port.

- **D7 — Launch catalog is the researched Tier 1 + Tier 2 set.** GitLab, GitHub, Atlassian, Linear, Notion, Sentry (DCR/token); Slack, Google Workspace family (confidential); Google Maps (token header). Each entry carries its setup hint (GitLab fine-grained PAT requirement, Atlassian admin-gated Basic, Google's AI-agent checkbox + Developer Preview enrollment).

## Risks / Trade-offs

- [Google Developer Preview terms bar use in public products; Workspace servers require program enrollment] → the catalog entry's setup hint states the prerequisite; the app is local personal software, and the entry degrades to instructions if Google changes terms.
- [Remote MCP endpoints churn (Atlassian v1→v2 cutover 2027-03, Stripe key migration 2026-12, SSE removals)] → endpoints are catalog data; a drift is a catalog edit, not a release. Atlassian initially targets `/v1/mcp` (streamable) with the v2 `?tools=all` flat-list form noted in the entry for the cutover.
- [`service` tag is an opaque string, so a hand-edited DB row could claim a service id] → harmless: the tag only drives tile matching; all behavior still flows through ordinary server mechanics.
- [Family rows duplicate the Google client secret per product] → acceptable at five products; revisit only if families grow large or secret rotation becomes painful.
- [Catalog in the frontend means two sources of truth for service facts (frontend catalog vs stored rows)] → the row stores what the user chose at connect time; later catalog edits do not mutate existing rows (same stance as provider base URLs).

## Migration Plan

One SQLite migration adding the nullable `service` column and new `auth` JSON fields; existing rows migrate trivially (null tag, unchanged auth). Rollback = drop the column; no data loss. Frontend-only catalog and pane additions are inert without backend support and vice versa (backend stores tags nothing else reads until the pane ships).

## Open Questions

None — scope and approach were settled during exploration (guided MCP connect only; ordinary-server coexistence; launch set as researched).
