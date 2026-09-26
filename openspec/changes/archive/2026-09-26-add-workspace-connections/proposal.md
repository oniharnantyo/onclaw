## Why

OnClaw agents cannot work external services (GitHub, Jira, Figma) on cloud deployments: local OpenClaw/Hermes style integrations rely on terminal CLIs and locally stored credentials that a server deployment does not have, and multi-tenancy forbids both shared local credentials and per-user authorization friction. OnClaw already ships the machinery — dual-transport MCP with workspace-scoped secret header rows, connection caching, probes, and per-agent server attachment — but wiring one service still means hand-configuring an MCP server. The product surface is missing: a one-press **Integrate** button per service.

## What Changes

- **Integrations tab in workspace settings**: a per-service gallery — recipe cards with Integrate buttons, a Connected section (status, attached agents, manage/disconnect), and the existing MCP management folded in as the "Custom MCP server" advanced card.
- **Server-side recipe registry**: declared per-service templates (service name/icon, auth type, transport endpoint, guided token-creation steps, recommended scopes, probe action, default access levels). Recipes are data; adding a service is registering a recipe, not editing core code. V1 registry contents: GitHub and GitLab available; Atlassian (one recipe covering both Jira and Confluence), Slack, and Linear declared coming-soon.
- **PAT connect flow (GitHub and GitLab live in v1)**: guided token creation, an access-level choice (read-only default, read & write one click away), the token stored as an encrypted secret header row, and the connect action gated on a passing probe. The GitLab recipe targets gitlab.com; self-managed GitLab rides the Custom card in v1.
- **Connection = product object that materializes an ordinary workspace MCP server**: creating a connection writes a linked `WorkspaceMCPServer` (streamable HTTP, prefilled URL, secret `Authorization` header, origin marker) so the connection cache, provider-safe naming, MCP policy, probes, and per-agent attachment all work unchanged; disconnect cascades the materialized server.
- **OAuth-class services ship as visible "Coming soon" cards** — Atlassian (one recipe covering both Jira and Confluence via the single remote MCP server), Slack, and Linear: recipes declared but requiring the OAuth lifecycle change; the gallery is truthful about what the button can do today.
- **Access level is stored and displayed** (read-only / read & write) and drives the guided token scopes; actual write enforcement rides the token scopes themselves. OnClaw-side role-based action gating (the user-authority inner wall) is an explicitly named follow-on, not part of this change.

## Capabilities

### New Capabilities

- `workspace-connections`: workspace-scoped service connections — recipe registry, Integrations gallery, PAT connect flow, connection lifecycle (probe, status, disconnect cascade), and per-agent attachment of managed connections.

### Modified Capabilities

- `members-roles`: add `integrations.write` to the closed permission catalog — granted to the built-in Owner and Admin roles (Superadmin via its all-workspace-permissions set), never to Member, held by custom roles only on explicit grant, and backfilled into the built-in roles of workspaces created before the permission existed.

<!-- Materialized MCP servers remain ordinary workspace MCP servers: workspace-mcp behavior is unchanged, and agent attachment reuses Agent.EnabledMCPS as-is. -->

## Impact

- **Domain** (`internal/domain`): connection entity, recipe descriptor types, access-level values.
- **Store** (`internal/store`, `internal/store/postgres`): new `workspace_connections` table; additive nullable `origin_connection_id` column on workspace MCP servers (migration 000062); in-memory fake updates.
- **Services/handlers** (`internal/services`, `internal/server/handlers`): connections service (create/list/disconnect/probe) reusing the MCP settings secret-row machinery; REST endpoints under the workspace scope.
- **MCP runtime** (`internal/agents/mcp`): no behavior change — connections materialize ordinary server rows; probe reused for connection health.
- **Web** (`web/`): new Integrations settings tab (gallery, connect dialog, connected section), agent config integrations section listing managed connections.
- **Smoke tests** (`scripts/smoke.sh`): connection lifecycle coverage.
