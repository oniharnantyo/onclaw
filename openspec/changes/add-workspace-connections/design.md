## Context

The MCP runtime already provides everything a service integration needs at the transport layer: dual-transport dialing (streamable HTTP with secret header rows), a workspace-scoped connection cache with singleflight and probes, provider-safe tool naming, per-agent attachment via `Agent.EnabledMCPS`, and secret handling (AES-256-GCM envelopes bound to the workspace, merge-on-name, last-4 hints). What does not exist is the product layer: a declared notion of "the GitHub integration", a gallery that walks a user from "press Integrate" to a connected service, and a connection object whose lifecycle (create/probe/disconnect) is coherent across the recipe, the stored token, and the materialized MCP server.

Workspace-scoped secrets encrypt with the workspace ID as AAD (`internal/agents/secretrows.go`); workspace MCP server management rides `domain.ToolsWrite` (reads ride membership, per the existing router wiring). Latest schema version is v61.

## Goals / Non-Goals

**Goals:**
- One-press connect for PAT-auth services, with GitHub as the v1 reference recipe.
- Service knowledge (endpoint, guided steps, scopes, probe) declared once, server-side.
- Zero new secret plumbing: reuse MCP secret-row machinery end to end.
- Zero new MCP-runtime behavior: connections ride the existing server rows, cache, naming, policy, and attachment.
- A gallery that is truthful: available, coming-soon, and custom paths visibly distinct.

**Non-Goals:**
- OAuth flows and token refresh (follow-on: `add-connection-oauth`).
- Connection-scoped HTTP transport for MCP-less APIs, e.g. Figma's cloud path (follow-on).
- Webhook ingress / reactive runs (follow-on).
- OnClaw-side role-based action gating on integration verbs — the user-authority inner wall (follow-on). V1 write enforcement rides token scopes.
- Per-user connections or "act as me" elevation.
- Shell credential injection (the scrubbed-env invariant stands).

## Decisions

- **D1 — A connection materializes an ordinary `WorkspaceMCPServer`.** The connection row (id, workspace, service, access level) links to a real workspace MCP server via a nullable `origin_connection_id` column. Alternatives: a first-class connection entity with its own consumer seams (rejected: duplicates dialing, caching, probing, naming, policy, and attachment that server rows already provide), or connection-as-pure-UI-view over hand-built MCP servers (rejected: no lifecycle coherence — disconnect would not cascade, tokens would not be owned). Consequence: agents, naming, policy, and probes need zero changes.
- **D2 — Recipes are declared Go data in a server-side registry**, served through a recipes endpoint. Web-side hardcoded cards rejected: the probe must run server-side anyway, guided copy is real copy, and a registry keeps future plugin-supplied recipes possible without core edits. V1 registry contents:
  - **GitHub — available.** PAT auth, official remote MCP endpoint. Reference recipe.
  - **GitLab — available.** PAT auth (granular PATs support read-only/read-write tiers), targeting gitlab.com. Transport — a hosted remote endpoint vs the official stdio server — is verified and pinned at apply time (task 5.3): a stdio recipe is representable (secret env rows exist for stdio) but additionally requires the server binary to be present in the deployment image, an operator provisioning concern, so a confirmed remote endpoint is preferred. Self-managed GitLab is excluded from v1 recipes: recipe endpoints must be server-declared data (D7), so self-hosted GitLab users ride the Custom card until a declared-instance recipe pattern exists.
  - **Atlassian — coming soon, one recipe covering both Jira and Confluence.** Atlassian's single remote MCP server serves both products from one OAuth 2.0 (3LO) connection; API tokens are not accepted by that server, so there is no PAT shortcut and no reason to model two cards.
  - **Slack, Linear — coming soon.** Official remote MCP servers, OAuth-kind auth.
  Coming-soon recipes are declared with `coming_soon` availability so the gallery renders them disabled and truthful.
- **D3 — The recipe's probe is an MCP tool call** through the existing probe path (e.g., GitHub's list-repositories tool), executed against the candidate connection *before* anything is stored; connect is probe-gated. Status persists on the materialized server row (its existing `Status`/`StatusError` fields); the connection view reads through.
- **D4 — The token lives as the single secret header row** (e.g., `Authorization`) on the materialized server, managed through the existing MCP settings service: encrypt-on-save, merge-on-name replacement, hint-only reads, decrypt at dial time only. The connections service composes the settings service; there is no second secret path and no new crypto.
- **D5 — Access level is connection metadata** (`read_only` default, `read_write`), driving guided scope copy and gallery display. Actual enforcement is the token's own scopes — a read-only fine-grained PAT cannot write regardless of what OnClaw believes, which is the strongest available gate. The OnClaw-side role gate is explicitly deferred so this change never claims enforcement it does not implement.
- **D6 — One connection per service per workspace** (unique on workspace + service). Multi-account (e.g., two GitHub orgs) deferred; the conflict error names the existing connection.
- **D7 — Connection endpoints come from the recipe registry, never user input.** The connect payload is (recipe id, access level, token) only. This change therefore adds no user-controlled URL surface — the SSRF exposure of custom MCP servers is pre-existing workspace-mcp behavior and out of scope.
- **D8 — Disconnect cascades in one transaction** (connection row, linked server row, agent attachment references) through the store's transaction seam; token ciphertext is deleted, not archived.
- **D9 — Web renders three surfaces from two endpoints** (recipes, connections): the Integrations settings tab (gallery + connected section), the connect dialog with guided steps, access-level preselected to read-only, and a probe-gated Connect button; and an Integrations section in agent config listing managed connections as attachable entries over the existing agent MCP attach endpoints.
- **D10 — Connection management gets its own closed-catalog permission, `integrations.write`, not `tools.write`.** Gating connections with the MCP-server permission would let any custom role carrying tool management also hold service credentials — custom roles are workspace-composable subsets of the catalog (`domain.Role.Permissions`). `integrations.write` defaults into the built-in Owner and Admin sets (Superadmin via its all-workspace-permissions set), is absent from Member, and reaches custom roles only by explicit grant — the same trust tier as `gateways.write` (credential-bearing admin surface, reads ride membership). Built-in role permissions are copied from catalog constants into role rows at workspace creation (`workspaces.go`, `admin_workspaces.go`) with no runtime derivation, so the schema migration also backfills `integrations.write` into existing workspaces' built-in Owner/Admin roles and the master tenant's Superadmin role.
- **D11 — Managed servers are read-plus-probe only outside Integrations.** Materialized servers appear in the MCP lists by design (that is the materialization architecture), but edit/delete on an origin-marked server would create two authorities over one credential — the connection would still claim a URL and token the operator just rewrote. Managed rows therefore reject edit/delete with a pointer error naming the owning connection; probe and status stay allowed (same machinery, and the two surfaces show one truth). Hand-made servers (null origin) are untouched.

## Risks / Trade-offs

- [Remote MCP endpoint outage looks like "our integration is broken"] → probe + status surface the failure with the upstream error; recipe endpoints are release-shippable data.
- [GitLab's best transport is unresolved until live verification] → task 5.3 pins remote vs stdio before release; if stdio wins, the recipe documents the required image provisioning for operators (OnClaw Cloud deployments would wait for a hosted endpoint instead of shipping stdio).
- [Recipe endpoint/auth facts drift upstream (GitHub, GitLab, Atlassian servers evolve)] → recipes are release-shippable data; probe failure surfaces upstream changes immediately; task 5.3 re-verification is part of the change's definition of done.
- [User creates a weaker token than the chosen access level; probe still passes] → guided scopes copy per access level; v1 probe validates tool presence, not write power; mismatch surfaces as tool errors in runs. Accepted for v1.
- [Coming-soon cards advertise unbuilt capability] → visible-but-disabled is a deliberate truthfulness choice; copy states the requirement (OAuth) without promising dates.
- [Tool-count/context budget when many services attach] → v1 targets one or two connections; a per-workspace attach budget is a follow-on policy, noted for the gallery's later scale.
- [Migration adds a column to a hot table] → additive nullable column with index; down migration drops it; no backfill (no existing connections).

## Migration Plan

1. Ship migration 000062 (workspace_connections table + origin column, plus the built-in-role backfill of `integrations.write`) with the release; it is additive and inert until a connection is created.
2. Rollback: standard down migration; a created connection's data is lost on rollback by design (tokens are not exported).
3. No data backfill, no dual-write, no feature flag beyond the registry contents (a service is "live" when its recipe says available).

## Open Questions

None blocking. The exact GitHub remote MCP endpoint URL and header shape are recipe-data verification, tracked as an implementation task (live-verified during apply, not spec-level).
