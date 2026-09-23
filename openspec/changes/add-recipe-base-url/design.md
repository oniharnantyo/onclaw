## Context

Recipes are process-lifetime singletons (`domain.RegisterRecipe`) whose `Endpoint` is copied verbatim into the materialized server (`services/connections.go:317`); typed params exist only for HTTP verb path/query values. The GitLab recipe (`recipes_builtin.go:302`) is MCP-kind, PAT-authenticated, pinned to `https://gitlab.com/api/v4/mcp` — live-verified as an OAuth-only endpoint (401 `WWW-Authenticate: Bearer scope="mcp"`), so the recipe cannot connect anywhere, while the same provider's REST API + PAT works on both gitlab.com and every self-managed instance. Self-managed ground truth from the explore: linkaja GitLab EE has no MCP feature (404, no `mcp` scope, no DCR); GHES has no remote MCP at all; ghe.com hosts are per-subdomain. See the explore memory (`mcp-oauth-connect-explore.md`) for the full probe record.

## Goals / Non-Goals

**Goals:**
- One new recipe-level concept (declared origin parameter) that makes any pinned-to-SaaS recipe self-managed-capable when the provider's API is origin-stable.
- GitLab connects today: re-scope to REST + PAT, killing the never-connectable wiring.
- Uniqueness that matches reality: same service, multiple origins.

**Non-Goals:**
- OAuth-recipe origin params (Atlassian/Slack/Linear authorize and token hosts are separate from API hosts; base-parametrizing them is a different problem) — PAT-kind recipes only in this change.
- GHES stdio preset around the official Go server (future recipe or Custom MCP card; the explore recommendation).
- GitLab MCP via OAuth — lands with `add-mcp-oauth-client` (DCR), after which an MCP-kind GitLab variant can be re-introduced.
- Per-verb origin overrides; multi-origin credential sharing (each connection holds its own PAT).

## Decisions

- **D1: Origin-only, path constants stay in the recipe.** The parameter accepts `scheme://host[:port]` and nothing else; GitLab's `/api/v4` prefix, GitHub's `/mcp/` path, verb paths — all remain recipe constants. Rationale: GitLab and GHES serve their APIs at fixed paths on the instance origin across all deployments, so origin captures exactly the variable part; letting users override paths would break verb declarations and webhook templates. Validation: parse with `net/url`, require http/https, empty `Path` (or "/"), no query/fragment/userinfo; normalize by stripping a single trailing slash before storing.
- **D2: `RecipeOriginParam{Name Default Help}` + resolved origin stored on the connection.** The materialized server keeps deriving from the recipe at connect time, but the connection row must remember the resolved origin (uniqueness checks + immutability enforcement + display) — the connection already persists lifecycle state, so this is a connection-view/store field, not a new table. Where the active `add-workspace-connections` schema lands the connection row, add a nullable `origin` column via a small migration in that change's lineage (schema is still pre-archive; coordinate the migration number at apply time).
- **D3: Uniqueness narrows at the service layer.** `ErrConnectionExists` (one-per-service) becomes one-per-(service, origin) where the recipe declares an origin param, unchanged otherwise. Rationale: keeps the existing guarantee for param-less recipes; the check lives where the current per-service check lives (connect-begin path), comparing stored origins.
- **D4: GitLab verb curation follows the existing tier machinery.** Read-only: projects list/get, issue list/get, MR list/get, branch list, pipeline list, user current. Read-write adds: issue create/update/comment, MR create/update/merge, pipeline retry/cancel. Undeclared → write (existing rule). Probe = current-user (cheap, read-only, exercises auth + origin). Webhook block untouched.
- **D5: GitHub param is opt-in UI, same machinery.** Declared like GitLab's; empty submission = default origin (SaaS), so existing flows are unchanged.

## Risks / Trade-offs

- [Conflict with the unarchived connection-change deltas (same code area, schema lineage)] → coordinate at apply time: those changes are implemented and committed; this change rebases on the shipped code and extends the migration sequence (000066+).
- [Existing (broken) GitLab connections in workspaces] → they can never have been connected; the re-scope surfaces as a new card. Migration: none; rows with service=gitlab keep working as inert rows until disconnected (status error was their only state).
- [Users expect OAuth "coming soon" semantics to flip for GitLab] → guidance text updated: PAT now, MCP-via-OAuth tracked by the follow-up change.

## Migration Plan

One small migration for the connection origin column (000067+, up/down). Deploy order: backend first (param ignored by current web until the dialog ships), web second. Rollback: revert; the column is additive and ignored by old code.

## Open Questions

- None blocking. Verb curation lists (D4) can be tuned at apply time against the real GitLab REST surface without changing the spec (the spec pins the tiers' existence and gating, not the exact verb list).
