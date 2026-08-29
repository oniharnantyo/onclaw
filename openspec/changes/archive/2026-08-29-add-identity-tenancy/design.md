# Design: add-identity-tenancy

Full technical plan: `~/.claude/plans/smooth-sauteeing-rain.md` (approved). This document summarizes the binding decisions; the plan file has exhaustive detail.

## Context

OnClaw's backend is greenfield. This change lands the first backend slice — multi-tenant configuration management (users, login, tenant switching) — and sets the architectural pattern for everything after it.

## Frameworks

gin (HTTP) · urfave/cli v3 (CLI) · pgx/v5 (Postgres) · golang-migrate (iofs + go:embed) · golang-jwt/v5 (HS256) · x/crypto argon2id · google/uuid

## Layout

```
main.go        package main — minimal: build root command, run, exit (no logic)
internal/
  cli/        root command construction, all commands (server, migrate, user,
              superadmin create, flags), drivers.go (blank imports: postgres store,
              local storage, password provider) — the composition root
  config/     Config struct; flags > env > defaults
  domain/     entities, sentinels, permission catalog + algebra
  auth/       Provider port+registry, password provider, argon2id, TokenIssuer (JWT HS256), Service
  server/     router, errors (sentinel→HTTP), middleware (auth/workspace/permission), api handlers
  store/      ports + registry + in-memory fake
  store/postgres  pool, executor iface (pool+tx both satisfy), WithTx, per-table files, embedded migrations
  storage/    port + registry + in-memory fake
  storage/local   local-disk driver under --data-dir
migrations/   000001_users … 000006_workspace_master (up/down pairs)
```

`internal/bootstrap` — fresh-instance lifecycle: `EnsureMaster` + `SeedSuperadmin`, invoked at server start (and callable from tests); idempotent, logged.

## Key decisions (locked in explore session)

1. **Stateless tenant scoping** — token = user, URL slug = tenant. No active-workspace server state; the switcher is client-side. `RequireWorkspace` middleware resolves slug → workspace + membership + role per request; non-members get 404 (indistinguishable from unknown slug — enumeration defense).
2. **Slug = immutable external ID** — renames change `name` only.
3. **JWT via TokenIssuer port** — HS256, TTL default 24h; per-request user load honors `disabled_at` immediately. Secret unset → ephemeral + loud warning (restarts invalidate sessions).
4. **Login provider registry** — `auth.Provider` port (Name / Authenticate / Begin / Complete / ProvisionsUsers); `password` ships first; SSO later = new implementation + blank import, zero core edits. Login request carries `provider` (default `password`); unknown → 400.
5. **ensureUser provisioning policy** — non-provisioning providers require an existing account (uniform 401); provisioning providers JIT-create from `Identity` (normalized email, `password_hash NULL`, `avatar_url` backfill when user has none). Direct-add and SSO land users in the same `password_hash NULL` state — one lifecycle.
6. **Configurable roles** — `roles` table per workspace; `permissions text[]` holds a closed catalog (Go constants): `workspace.read|write`, `members.read|write|remove`, `roles.read|write`. Built-ins immutable: Owner (all 7 + `is_owner`), Admin (6), Member (3 reads).
7. **Permission-set algebra** — edit needs `target ⊊ actor`; assign needs `role ⊆ actor`; peers not manageable; `is_owner` used ONLY by last-owner guard + transfer. Last-owner guard: refuse demote/remove when no OTHER member holds an owner role.
8. **Avatar = storage port + local driver** — capability URLs (128-bit random names) so `<img src>` works without auth headers; `avatar_key` (key, not URL) + `avatar_url` (external, SSO backfill); magic-byte type check (`http.DetectContentType`), ≤2MB; DB-fail-after-put → best-effort delete (no orphans).
9. **Adapters extend stored semantics, not DB features** — no citext (email lowercased at port boundary), no triggers (app-managed `updated_at`), pgx error codes translated to domain sentinels at the adapter (23505 → `ErrConflict`); sentinels cross the boundary, never pgx types.
10. **Testing** — in-memory fakes for fast handler/service tests; build-tagged integration tests vs dockerized Postgres; e2e smoke via curl script.
11. **Flat entrypoint** — `main.go` at repo root, minimal (build root command via `internal/cli` → run → exit, no logic); all wiring including driver blank imports lives in `internal/cli`, the composition root.
12. **Master-tenant control plane** — the superadmin is a member of a default `master` tenant (reserved slug, `is_master` flag) holding the built-in `Superadmin` role (`is_owner=true`, `admin.*` permissions). Admin routes (`/api/v1/admin/*`) are guarded by master-tenant membership + `admin.*` permission via the same middleware chain — **no `instance_admins` table, no new middleware concept, no bypass**. Inside non-master tenants a superadmin is an ordinary user (404 like any non-member).
13. **Fresh-instance seeding from env** — `ONCLAW_SUPERADMIN_EMAIL` + (`ONCLAW_SUPERADMIN_PASSWORD` | `ONCLAW_SUPERADMIN_PASSWORD_FILE`) — at server start, `internal/bootstrap` runs `EnsureMaster` (idempotent: reserved slug `master`, `is_master=true`, built-in `Superadmin` role with `admin.*` perms + `Member`) and `SeedSuperadmin` (only when master has zero Superadmin members; env ignored + logged once seeded; password never logged). CLI alternative: `onclaw superadmin create`. The former `bootstrap` and `workspace create` CLI commands are **dropped** — tenant creation + owner assignment is an admin-API operation.
14. **Admin.* catalog additions** — `admin.workspaces.read|write`, `admin.users.read|write`, `admin.superadmins.write`, assigned only to the master tenant's built-in `Superadmin` role. `WorkspaceStore` gains an unscoped `ListAll` (admin-only path — the one deliberate bend of the tenant-scope rule, permission-gated). `workspaces.is_master` column + seed logic in Go (single source of truth = catalog constants; no SQL-seeded roles).

## Store interfaces (binding shapes)

```go
type Store interface {
    Users() UserStore
    Workspaces() WorkspaceStore
    Roles() RoleStore
    Members() MemberStore
    WithTx(ctx context.Context, fn func(Store) error) error
    Close() error
}
```
Sub-port methods as in the plan (Create/ByEmail/ByID/List/SetDisabled/SetPasswordHash; Create/BySlug/Update/ListForUser/**ListAll (unscoped, admin path)**; Create/ByID/ListForWorkspace/CountMembers; Add/Get/ListForWorkspace/ListForUser/UpdateRole/Remove).

## API surface

Plan's table plus the admin group (all `/api/v1`):

| Route | Guard | Behavior |
|---|---|---|
| GET `/admin/workspaces` | `admin.workspaces.read` | all workspaces incl. suspended, member counts |
| POST `/admin/workspaces` | `admin.workspaces.write` | create + assign owner by email (auto-create passwordless user on unknown email) |
| POST `/admin/workspaces/:ws/disable` · `/enable` | `admin.workspaces.write` | suspend/restore (master protected) |
| GET `/admin/users` | `admin.users.read` | list users |
| POST `/admin/users` | `admin.users.write` | create user (email/name/password) |
| POST `/admin/users/:uid/disable` · `/enable` | `admin.users.write` | global enable/disable |
| POST `/admin/superadmins` · DELETE `/admin/superadmins/:uid` | `admin.superadmins.write` | grant/revoke Superadmin role in master; last-admin guard |

Error envelope as per plan. Suspended workspaces reject member requests with 403 (memberships retained).

## CLI

`server` (runs internal/bootstrap ensure+seed at start) · `migrate up|down|status|version` · `user create|list|disable` · `superadmin create` (explicit CLI seeding alternative). Dropped: `bootstrap`, `workspace create`.

## Non-goals

Impersonation · audit log · custom-role CRUD endpoints · SSO providers (port ready) · password-set flow · pagination · S3 storage driver · auto-migrate at server start (operator runs `onclaw migrate up` first).
