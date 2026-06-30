## Context

`onclaw serve` (`internal/cli/serve_cmd.go`) starts an HTTP server built from
the `internal/web` package. Today that package is flat: a `Server` struct holds
`*sql.DB`, `*llm.Service`, five `store.*` interfaces, an in-memory session map,
and a cookie key; its handler methods embed business logic and, in six places,
issue raw SQL against the `preferences` table.

The store layer (`internal/store/`) already defines clean interfaces
(`KVStore`, `ConversationStore`, `ProfileStore`, `SecretStore`, `AgentStore`)
and `*llm.Service` is the CRUD facade the handlers actually use
(`GetProfile`/`AddProfile`/`GetSecret`/`GetAgent`/…). The codebase follows
contract/types/impl separation (`.claude/rules/coding-style.md`) and prefers
formal store methods over direct SQL.

Shipped state (reference, not work to redo):
- **Store interfaces** (`internal/store/store.go`): `KVStore.Get/Set/Delete`,
  `ConversationStore.CreateConversation/AppendMessage/ListMessages/ListConversations/…`.
- **CRUD facade** (`internal/llm/service.go`): `*llm.Service` exposes profile,
  secret, and agent CRUD. It has **no** `UpdateProfile` (provider update is done
  via remove + re-add), and returns `fmt.Errorf("… not found")` with no sentinel
  on cache misses (`GetProfile` line 139, `GetAgent` line 289).
- **Transport** (`internal/web/`): `Server`, routes, handlers, auth (session +
  bcrypt + CSRF), SSE writer, embedded SPA assets.

## Goals / Non-Goals

**Goals:**
- Separate transport from domain logic into compiler-enforced layers.
- Eliminate raw SQL from production handlers; route all data access through
  `store.*` interfaces.
- Remove dead code (the three unused store fields, the unused cookie key).
- Rename the Go package `web` → `api` to disambiguate from the frontend `web/`.
- Preserve all observable behavior (same routes, auth, JSON shapes, SSE stream).

**Non-Goals:**
- Changing any API wire format, route, or response shape.
- Adding `UpdateProfile` to `llm.Service` (the provider-update remove/re-add
  dance is preserved, only centralized).
- Introducing sentinel errors in `llm.Service` (not-found stays classified by
  message — collapsed from six sites to one helper, not eliminated).
- Changing authentication behavior. (Note: session cookies are not
  cryptographically signed today; this refactor preserves that behavior as-is
  and does not fix it.)
- Touching the frontend `web/` Vite project beyond its build-output path.

## Decisions

### 1. Two layers: transport (`api` + sub-packages) and domain (`api/service`)

**Decision:** Split into a domain package `internal/api/service` and a transport
layer composed of `internal/api` (root) plus three sub-packages `handler`,
`httpx`, `auth`.

**Rationale:** A real Go package boundary makes layer separation a compiler
error if violated. `service` cannot import `net/http` or `database/sql`, so
business logic physically cannot reach the HTTP request or the database
connection. This continues the repo's existing contract/types/impl discipline.

### 2. `httpx` is a forced leaf package

**Decision:** Move `JSON`/`Error`/`SSEWriter` into `internal/api/httpx`, a
stdlib-only leaf.

**Rationale:** Handlers need these helpers. If they remained in `package api`,
then `handler` → `api` (for the helpers) while `api` → `handler` (route wiring)
— an import cycle. Promoting them to a leaf that nothing depends on upward
breaks the cycle. This single constraint dictates the sub-package shape.

### 3. Handlers are methods on `handler.Handler`, not `*Server`

**Decision:** Resource handlers become methods on a `Handler{ svc *service.Service }`
struct in `internal/api/handler`.

**Rationale:** Go methods must live in the same package as their receiver type.
To place handlers in their own folder, they cannot remain methods on
`api.Server`. A small `Handler` holding only the service dependency keeps them
independently testable and lets route wiring reference
`server.handlers.ListProviders`.

### 4. Auth splits into `SessionStore` + middleware + login handlers

**Decision:** Extract the in-memory session map into `auth.SessionStore`; move
`requireAuth` (CSRF + session check) to `auth.RequireAuth`; move login/logout to
`auth.Login`/`auth.Logout`. Password verification lives in
`service.VerifyPassword`.

**Rationale:** Session/cookie/CSRF are HTTP concerns (transport); password
verification reads a preference and runs bcrypt (domain). Splitting them lets
`auth` depend on `httpx` + `service` without pulling in the whole server, and
keeps `api.Server` a thin composition root.

### 5. Preference reads go through `KVStore`; not-found is centralized

**Decision:** Replace the six `s.db.QueryRowContext("SELECT … preferences …")`
calls with `kv.Get(ctx, key)` inside `service`. Introduce `service.ErrNotFound`
and one `classify(err)` helper; handlers map `errors.Is(err, service.ErrNotFound)`
to HTTP 404.

**Rationale:** The store abstraction already exists; using it removes `*sql.DB`
from the server entirely. `llm.Service` returns a formatted `"… not found"`
error with no sentinel today; rather than expand scope into `llm.Service`, the
string sniff is collapsed from six call sites into one helper. (Adding real
sentinels to `llm.Service` is a clean follow-up, explicitly out of scope.)

### 6. Provider update keeps the remove → re-add → restore-secret dance

**Decision:** `service.Update` preserves the existing read-secret → remove →
re-add → restore-secret flow.

**Rationale:** `llm.Service` has no `UpdateProfile`. Changing that is a separate,
larger change. Centralizing the dance in one service method is strictly better
than today (where it lives inline in a handler) without expanding scope.

### 7. Rename `web` → `api`; keep the frontend `web/`

**Decision:** `git mv internal/web internal/api`; the root `web/` Vite project is
untouched. The Vite `outDir` (`web/vite.config.ts`) moves from
`../internal/web/assets` to `../internal/api/assets`.

**Rationale:** `api` describes the Go package's primary job (the JSON API server
under `/api/*`); it incidentally also serves the compiled SPA. The frontend
source keeps its natural name `web`. Only one importer (`serve_cmd.go`) and one
build path (`vite.config.ts`) reference the old path, so the rename is isolated.
Committed build assets move with the directory and must not be gitignored.

## Risks

- **Import cycles.** The dependency graph (`httpx` ← `auth`/`handler`;
  `api` → `auth`/`handler`/`httpx`/`service`) is acyclic by construction;
  `go build ./...` will fail loudly if violated. Keep `AssembledAgent` and
  `ResolveAndAssembleFunc` in `service`, not `api`.
- **Test churn.** `server_test.go` is white-box (`package api`) and decodes into
  unexported DTOs. Exporting those types as `service.*View`/`*Input` keeps test
  bodies unchanged; only type qualifiers and `setupTestServer` change.
- **Embedded assets path.** `//go:embed assets/*` is file-relative and moves with
  the directory; verify `make ui` still writes to `internal/api/assets/` after
  the `vite.config.ts` edit.
