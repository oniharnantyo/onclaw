## Why

Integration recipes hardcode a single provider endpoint (`gitlab.com/api/v4/mcp`, `api.githubcopilot.com/mcp/`), so self-hosted OnClaw deployments — the product's core audience — cannot connect to self-managed provider instances. Live probes of a real self-managed GitLab (gitlab.linkaja.com) show the remote MCP feature is absent there (GitLab 18.x beta, admin-enabled), while its REST API + PAT works today; GitHub Enterprise Server has no remote MCP at all. The recipe layer needs one new concept — a declared base-URL parameter — and the GitLab recipe needs re-scoping to the REST API so it actually connects (its current PAT-vs-MCP-endpoint wiring can never succeed, even on gitlab.com).

## What Changes

- Recipes can declare an optional **base-URL parameter**: the connect flow renders a labeled origin field preset with the SaaS default, and the recipe's endpoint/verb paths are resolved against it at materialization. Origin-only values (scheme + host), validated; immutable after connect.
- Per-service connection uniqueness narrows to **per (service, resolved origin)**: a workspace can hold `gitlab.com` and `gitlab.linkaja.com` connections side by side; the same origin twice is still rejected.
- **GitLab recipe re-scoped to HTTP-kind REST** (`AuthKind: PAT`, origin param default `https://gitlab.com`): curated read-only / read-write verb surface over `/api/v4` (projects, issues, merge requests, branches, pipelines), declared-call probe, existing webhook signing kept. This replaces the broken PAT-against-`/api/v4/mcp` wiring — **BREAKING** for anyone who had a (non-functional) GitLab connection; disconnect + reconnect required.
- **GitHub recipe gains the optional origin param** (default `https://api.githubcopilot.com`), enabling GitHub Enterprise Cloud data-residency hosts (`copilot-api.<subdomain>.ghe.com/mcp`). GHES remains unsupported for remote MCP — guidance text says so explicitly.

## Capabilities

### New Capabilities

- `connection-recipes`: the integration catalog contract — recipe declarations (transport, auth kind, endpoint/verb paths, scopes, probe, webhooks), the base-URL parameter (validation, derivation, immutability), and per-(service, origin) connection uniqueness.

### Modified Capabilities

- (none — recipe-level behavior is currently specified only in active, unarchived change deltas; this change introduces the recipe contract as its own capability rather than deltaing specs that don't exist in `openspec/specs/` yet)

## Impact

- `internal/domain/recipes.go` (Recipe struct: base-param fields + validation), `recipes_builtin.go` (GitLab re-scope with curated verbs; GitHub origin param)
- `internal/services/connections.go` (derivation at materialization; uniqueness narrowing — locate the per-service check), probe path for HTTP-kind declared calls on parametrized origins
- `internal/server/handlers/connections.go` (connect request carries the origin param)
- Web: connect dialog renders the origin field for recipes that declare it (Integrations gallery → connect dialog)
- No DB migration: origins resolve into the materialized server's existing URL/secret rows and the connection row is unchanged
