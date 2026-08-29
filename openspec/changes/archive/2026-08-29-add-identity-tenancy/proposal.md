# Proposal: add-identity-tenancy

## Why

OnClaw's backend is greenfield — every future feature (agents, channels, cron, runs) is blocked on the tenant boundary and identity. This change lands the first backend slice so later work has the architectural patterns (ports/adapters, per-table adapters, registry-based replaceability) to slot into.

## What Changes

- **New backend skeleton** (Go + gin + urfave/cli v3 + pgx/v5 + golang-migrate + golang-jwt + argon2id):
  - `internal/domain` — entities, sentinel errors, permission catalog + algebra
  - `internal/store` ports + `postgres` adapter (per-table files) + in-memory fake
  - `internal/storage` port + `local` driver (avatars) + fake
  - `internal/auth` — `TokenIssuer` (JWT HS256) and login `Provider` port + registry (`password` first; SSO providers register later without core edits)
  - `internal/server` (gin, `/api/v1`) — middleware chain, handlers, sentinel→HTTP error mapping
  - `internal/cli` — `server`, `migrate`, `user`, `workspace`, `bootstrap`
- **Database schema** via 5 numbered migrations: `users`, `workspaces`, `roles`, `workspace_members`, plus `avatar_key`/`avatar_url` columns
- **User avatars** via upload: `storage` port + local-disk driver, capability URLs, magic-byte type checking
- **API surface** (`/api/v1`): auth (provider-pluggable login, me), workspaces (create/get/update/list), members (list/add/update/remove with permission-set guards), roles (list), self profile + avatar upload/serve
- **Instance admin via a default master tenant** — the control plane is ordinary domain data: migration/`internal/bootstrap` ensures a reserved `master` tenant (flagged `is_master`) with a built-in `Superadmin` role (`is_owner`, `admin.*` permissions); fresh instances seed the first superadmin **from env** (`ONCLAW_SUPERADMIN_EMAIL` + password/password-file), idempotently; `/api/v1/admin/*` routes let the superadmin list/create/suspend tenants, assign owners (auto-creating passwordless users on unknown email), and manage users and other superadmins — with no bypass of tenant scoping outside master
- **CLI** — `server` (runs ensure-master + seed at start), `migrate`, `user`, `superadmin create`; the earlier `bootstrap`/`workspace create` commands are dropped (superseded by env seeding + admin API)
- **Docs**: CLAUDE.md/AGENTS.md updated — backend no longer greenfield

## Capabilities

### New Capabilities
- `user-accounts`: account lifecycle + login via pluggable providers — CLI/direct-add/JIT creation, password login, profile + avatar, disable; SSO providers register via the Provider port
- `tenancy`: workspaces as tenants — immutable slugs, stateless tenant scoping (token=user, URL=workspace), creation flow, settings, switcher payload
- `members-roles`: workspace membership + configurable roles — permission catalog + set algebra (edit/assign/guards), built-in seeded roles, member management endpoints
- `instance-admin`: default master tenant — env-seeded first superadmin, `admin.*` permission catalog, admin routes for tenant/user/superadmin management, master-tenant protections, last-admin guard, no-bypass rule

### Modified Capabilities
- `web-app`: the members settings screen and workspace switcher eventually consume the new `/api/v1` endpoints — but no web changes land in this change. No spec-level web behavior changes here, so list only as future integration; no delta spec.

## Impact

- **Code**: root `main.go` (minimal entrypoint) + `internal/{domain,auth,server,store,storage,cli,config}`, `migrations/`
- **Dependencies**: gin, urfave/cli v3, pgx/v5, golang-migrate/v4, golang-jwt/v5, x/crypto (argon2id), google/uuid
- **Infra**: requires Postgres 13+; integration tests need dockerized Postgres
- **Docs**: CLAUDE.md/AGENTS.md "Repository State" + Commands sections change from "no backend code yet" to the real layout
- **Ops**: new env surface (`DATABASE_URL`, `ONCLAW_*`), data dir for local storage driver, single-binary deployment (`onclaw server`)

## Non-goals

First SSO provider implementation (OIDC) and its begin/callback routes (port + registry only) · password-set flow for direct-added/SSO-provisioned users · JWT revocation denylist/refresh · login rate limiting · role CRUD endpoints (storage + guards exist) · audit log · pagination · S3 storage driver · avatar resizing · web-app integration · user deletion (disable only) · user email change.
