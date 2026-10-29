# Proposal

## Why

Connecting buddies to an external service today means hand-configuring a raw MCP server definition — the user must discover the endpoint URL, pick a transport, and know which authentication the server expects. Every major service (GitLab, GitHub, Atlassian, Google Workspace, Notion, Slack, …) now publishes an official remote MCP server with standardized OAuth, so the app can invert the axis: the user picks a *service*, and buddies supplies the endpoint, auth method, and setup guidance. The exploration (2026-10-27) verified 20+ first-party remote MCP servers and found buddies' existing MCP + OAuth machinery already covers the transport/token layer; what is missing is the service catalog and guided connect flow.

## What Changes

- Add an **Integrations pane** to settings: a filter rail (category chips — All/Coding/Productivity/Communication/Business/Utilities — plus a search input) over a flat grid of icon tiles, one per catalog service, each showing a connection status dot.
- Add a **frontend service catalog** (mirroring the provider type catalog pattern): per service — id, label, brand icon, category, search keywords, endpoint (fixed or instance-URL template), declared auth methods, credential hint text, and setup instructions.
- Add a **connect sheet** per service: collects what the catalog declares (instance URL, auth method choice, credential or pre-registered OAuth client ID/secret), then creates a catalog-tagged MCP server definition and probes it. Google Workspace is a **family entry**: one shared setup (its own OAuth client + product checklist) that creates one MCP server definition per selected product.
- Add a **confidential-client OAuth path** to the MCP OAuth machinery: when the server definition stores a pre-registered client ID/secret, authorization uses that client instead of dynamic client registration — required for Google (no DCR), Slack, Asana, Box, and Dropbox.
- Add **per-service header auth styles** for token-based MCP connections: `Authorization: Bearer`, `Token token=`, Basic `email:api_token`, custom schemes (`Sentry-Bearer`), and non-Authorization headers (`X-Goog-Api-Key`).
- Keep integration credentials **secret**: a token or API key entered through the connect sheet is stored server-side and injected at dial time; service reads and listings expose only key-set state and a last-4 hint, following the provider credential secrecy pattern (raw MCP headers currently round-trip to the frontend — integration credentials must not).
- Launch catalog: GitLab, GitHub, Atlassian, Linear, Notion, Sentry (Tier 1 — works with existing OAuth machinery); Google Workspace family and Slack (Tier 2 — confidential-client mode); Google Maps Grounding Lite (static header). Tier 3 services (Vercel client allowlist, ClickUp rate caps, Asana/Box/Dropbox without confidential mode) are catalog data added later, not code.

Out of scope: non-MCP credential consumers (a general credential vault with no consumer yet), Telegram (an inbound gateway trigger, not an integration), Microsoft 365 and YouTube (no official MCP servers), per-service tool filtering beyond what the MCP server itself exposes.

## Capabilities

### New Capabilities

- `integrations`: the guided service-integration catalog — the Integrations pane (filter rail, icon-tile grid, status dots), the connect sheet and family flow, declared auth methods (OAuth DCR, OAuth confidential client, styled header credentials), credential secrecy for integration tokens, and the relationship between integration rows and MCP server definitions.

### Modified Capabilities

- `settings`: the settings navigation requirement gains the Integrations pane as a sidebar entry alongside Providers, MCP, Skills, Tools, and Hooks.
- `agent-mcp`: the OAuth server authentication requirement gains the pre-registered (confidential) client path — when the stored server definition carries a client ID/secret, authorization uses it instead of dynamic registration.

## Impact

- **Frontend**: new `frontend/src/features/settings` Integrations pane component; extended `brandMarks` glyph map for service icons; new catalog module (frontend-only, presentation source of truth); connect sheet component.
- **Backend**: `store.MCPServer` gains a service tag and integration credential storage (auth JSON extension) with a SQLite migration; `internal/agents/mcp` authorize path learns the confidential-client branch; `internal/services` MCP server reads/listings redact integration credentials (KeySet + hint); bindings regenerated.
- **No runtime behavior change** for existing MCP servers: rows without a service tag behave exactly as today; the MCP pane remains the raw/custom escape hatch and continues to list integration-created servers.
- **Dependencies**: none new (OAuth flow, HTTP transports, and SQLite all in place).
