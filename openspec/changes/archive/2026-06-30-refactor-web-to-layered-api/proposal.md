## Why

`internal/web/` is a single flat package (~1,750 LOC across 11 files) where HTTP
transport, business logic, and data access are fused. This produces three
concrete problems:

1. **Raw SQL bypasses the store layer.** Six handlers call
   `s.db.QueryRowContext(ctx, "SELECT value FROM preferences WHERE key = '...'")`
   to read the `default_agent` / `default_provider` preferences, instead of the
   existing `store.KVStore.Get` interface (`internal/store/store.go`). The
   *write* side already uses `kvStore.Set(...)` — only the read side reaches past
   the abstraction into `*sql.DB`.
2. **Dead code.** `Server.agentStore`, `.profileStore`, and `.secretStore` are
   constructed in `NewServer` but never referenced (all CRUD flows through
   `*llm.Service`). `Server.cookieKey` is generated with `crypto/rand` but never
   read.
3. **Fragile error classification.** Six handlers decide 404-vs-500 with
   `strings.Contains(err.Error(), "not found")`, because `llm.Service.GetProfile`
   / `GetAgent` return `fmt.Errorf("… not found")` with no sentinel error.

The package name `web` also collides with the **frontend** Vite project at the
repo root (`web/`), which is the SPA source whose build output the Go package
embeds. Renaming the Go package to `api` removes the ambiguity.

## What Changes

- **Rename** the Go package `internal/web` → `internal/api`: directory, package
  declarations, the single importer `internal/cli/serve_cmd.go`, and the Vite
  build target `web/vite.config.ts` `outDir`. The frontend `web/` project is
  untouched.
- **Split into compiler-enforced layers:**
  - `internal/api/service/` — domain logic. Depends only on `store.*` interfaces
    and `*llm.Service`; never imports `database/sql` or `net/http`. Houses the
    provider/agent/conversation/chat operations, view assembly, password
    verification, and a single not-found error classifier.
  - `internal/api/handler/` — thin HTTP handlers (decode → service → encode) as
    methods on a `Handler` struct.
  - `internal/api/httpx/` — leaf package with `JSON`/`Error`/`SSEWriter` helpers
    (stdlib only). Extracted to a leaf so `handler` can use it without importing
    `api` (which would cycle via route wiring).
  - `internal/api/auth/` — `SessionStore`, the `RequireAuth` middleware (CSRF +
    session), and the login/logout handlers.
  - `internal/api/` (root) — a slim `Server` composition root: `NewServer`,
    `ListenAndServe`, route wiring, embedded assets, and the SPA fallback.
- **Remove dead code:** the three unused store fields and `cookieKey`.
- **Route preference reads through `store.KVStore`** — eliminating all six raw
  SQL queries from production code.
- **Centralize not-found classification** into one `service.ErrNotFound` +
  `classify(err)` helper, replacing the six inline `strings.Contains` checks.

## Capabilities

### Modified Capabilities

- `web-ui`: No change to observable behavior — `onclaw serve` still serves the
  embedded SPA at `/` and the JSON API under `/api/*`, all management routes
  still require passphrase auth, chat still streams over SSE, and
  provider/agent/conversation CRUD still delegates to `llm.Service`. The change
  adds one new **architectural** requirement capturing the layer-separation
  invariant the refactor establishes (separate transport/domain packages; no
  `net/http` or `database/sql` in the domain layer; no raw SQL in handlers; the
  Go package renamed `web` → `api`). See `specs/web-ui/spec.md`.

### New Capabilities

_None._

### Removed Capabilities

_None._

## Impact

- **Code**: `internal/web/**` → `internal/api/**` (rename + restructure into
  `service/`, `handler/`, `httpx/`, `auth/` sub-packages);
  `internal/cli/serve_cmd.go` (composition-root rewiring);
  `web/vite.config.ts` (build-output path).
- **API**: No wire-format change. Request/response JSON shapes are preserved —
  structs move from unexported handler DTOs to exported `service` view/input
  types with identical field names and JSON tags.
- **CLI**: `onclaw serve` unchanged in behavior; only its internal wiring changes.
- **Database**: None. No schema changes, no migrations.
- **Compatibility**: Fully backward compatible. All seven existing HTTP tests
  (`TestWebHealth`, `TestWebAuth`, `TestWebProvidersCRUD`, `TestWebAgentsCRUD`,
  `TestWebConversations`, `TestWebStaticSPA`, `TestWebSSEChat`) pass with
  unchanged assertions.
