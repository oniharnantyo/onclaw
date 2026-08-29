## 1. Foundation

- [x] 1.1 Add Go dependencies and wire root `main.go` (minimal: build root command via `internal/cli`, run, exit — no logic) with `internal/cli` as composition root (root `cli.Command`, all commands, `drivers.go` blank-importing the postgres store, local storage, password auth provider)
- [x] 1.2 `internal/domain`: User/Workspace/Role/Member/MemberView types, sentinel errors, permission catalog constants (workspace/members/roles + `admin.*` entries), permission algebra (canEdit/canAssign/lastOwner rule), slug + email validation — table-driven tests
- [x] 1.3 Migrations `000001_users` → `000006_workspace_master` (up/down pairs; 000006 adds `workspaces.is_master`)
- [x] 1.4 `internal/config`: Config struct + per-command flag→config assembly

## 2. Ports & Fakes

- [x] 2.1 `internal/store`: `Store` + 4 sub-ports, `DSNConfig`, registry (`Register`/`Open`); `WorkspaceStore.ListAll` (unscoped, admin path); document sentinels-across-boundary and email-normalization invariants
- [x] 2.2 `internal/store/fake`: in-memory Store with tx support (clone-on-WriteTx snapshot model)
- [x] 2.3 `internal/storage`: `Storage` port (Put/Open/Delete/URL), `StorageConfig`, registry; `internal/storage/fake`
- [x] 2.4 `internal/storage/local`: data-dir driver (atomic put via temp+rename), random capability-key generation

## 3. Postgres adapter

- [x] 3.1 `internal/store/postgres/postgres.go`: pgxpool, executor interface (pool + tx both satisfy), WithTx, MigrateUp/Status via go:embed + iofs
- [x] 3.2 `users.go`, `workspaces.go`, `roles.go`, `workspace_members.go`, `convert.go`; pg error translation (23505 → ErrConflict)
- [x] 3.3 Build-tagged integration tests vs dockerized Postgres: migrate up/down idempotence, per-store CRUD, WithTx rollback, conflict mapping

## 4. Auth

- [x] 4.1 `internal/auth/provider.go` — Provider port + registry (Name/Authenticate/Begin/Complete/ProvisionsUsers, Identity)
- [x] 4.2 `password_provider.go` + `password.go` — uniform-401 semantics, argon2id PHC encode/verify
- [x] 4.3 `issuer.go` (TokenIssuer port + Claims) + `jwt.go` (HS256, TTL config, ephemeral-secret warning)
- [x] 4.4 `service.go` — Login(provider) routes registry → ensureUser provisioning policy → Issue; Me payload; tests with fake provider (unknown provider 400, provisioning vs not, avatar backfill)

## 5. HTTP server

- [x] 5.1 `errors.go` — envelope + sentinel→status map (invalid_request/unauthenticated/forbidden/not_found/conflict/last_owner_protected/internal), 404-over-403 for non-members
- [x] 5.2 middleware: AuthRequired (verify → load user → disabled reject), RequireWorkspace (slug→ws+member+role, non-member 404, suspended → 403), RequirePermission
- [x] 5.3 api handlers: auth (login/logout/me), workspaces (list/create/get/patch), members (list/add/patch/delete with guards), roles (list), users (PATCH me, avatar upload with magic bytes ≤2MB, serve), files (capability serving + cache headers), respond helpers
- [x] 5.4 router.go + httptest suite (store/fake + storage/fake): endpoint matrix incl. guards, 404-over-403, uniform 401s, upload matrix, capability serving

## 6. CLI

- [x] 6.1 flags.go (DATABASE_URL + ONCLAW_* env Sources) + server.go (listen, JWT secret fallback warning, run internal/bootstrap at start)
- [x] 6.2 migrate.go (up/down/status/version)
- [x] 6.3 user.go (create [--avatar-file], list, disable) + superadmin.go (explicit CLI seeding, same idempotence rules as env seeding)

## 7. Docs + verification

- [x] 7.1 Update CLAUDE.md + AGENTS.md: Repository State (no longer greenfield), backend commands, layout notes
- [x] 7.2 `go build ./... && go vet ./... && go test ./...` green
- [x] 7.3 Integration tests vs dockerized Postgres if available; e2e smoke: env-seed superadmin → login → me → admin create tenant + owner → suspend/restore → member mgmt → guards → avatar upload/serve → last-owner + last-admin guards

## 8. Instance admin (master tenant)

- [x] 8.1 `domain/permissions.go` admin.* constants wired into master `Superadmin` role seed; `workspaces.is_master` handling + slug `master` reserved list
- [x] 8.2 `internal/bootstrap` — `EnsureMaster` (idempotent master tenant + Superadmin/Member built-ins) + `SeedSuperadmin` (env/password-file, idempotent, never logs secret); `internal/cli/superadmin.go` uses the same seeding service
- [x] 8.3 Admin route group: fixed-master variant of the workspace middleware, permission guards, handlers (workspaces list/create/disable-enable incl. member counts + owner assignment, users list/create/disable-enable, superadmins grant/revoke), `WorkspaceStore.ListAll` in both store fake and postgres adapter
- [x] 8.4 httptest matrix: fresh-instance seeding, idempotent re-seed, master protection (suspend/rename/slug), owner assignment (existing + auto-created user), suspend→403 members→restore, global user disable, last-admin guard, superadmin-no-bypass 404
- [x] 8.5 integration: ensure/seed idempotence + is_master column against dockerized Postgres
