## 0. OpenSpec + scaffolding

- [x] 0.1 Capture this change under `openspec/changes/refactor-web-to-layered-api/`
- [x] 0.2 `openspec change validate refactor-web-to-layered-api` passes

## 1. Rename `internal/web` → `internal/api` (behavior-preserving)

- [x] 1.1 `git mv internal/web internal/api`
- [x] 1.2 `package web` → `package api` in all 11 `.go` files
- [x] 1.3 `web/vite.config.ts` `outDir` → `../internal/api/assets`
- [x] 1.4 `internal/cli/serve_cmd.go`: import path `internal/web` → `internal/api`; `web.*` → `api.*` (`NewServer`, `AssembledAgent`, `ResolveAndAssembleFunc`)
- [x] 1.5 Verify: `go build ./...`, `go vet ./...`, `go test ./internal/api/...`; `git log --follow internal/api/server.go`

## 2. Domain layer `internal/api/service`

- [x] 2.1 `types.go` — export view/input structs (`ProviderView`, `AgentView`, `SecretStatus`, `ProfileInput`, `AgentInput`, `SetSecretInput`, `ChatInput`); move `AssembledAgent` + `ResolveAndAssembleFunc`
- [x] 2.2 `errors.go` — `ErrNotFound` + `classify(err)` helper (maps `llm.Service` "… not found" and `sql.ErrNoRows`)
- [x] 2.3 `service.go` — `Service{ mgr, kv, conv, resolve, log }` + `New(...)`
- [x] 2.4 `provider.go` — 8 ops; `IsDefault`/`SecretSet` assembly; `Update` (remove→re-add→restore-secret); `kv.Get` for default reads
- [x] 2.5 `agent.go` — 5 ops; `kv.Get` for default reads; delete-time default cleanup
- [x] 2.6 `conversation.go` — `List` / `Messages`
- [x] 2.7 `chat.go` — `Chat(ctx, ChatInput) (convID int64, a AssembledAgent, err error)`
- [x] 2.8 `auth.go` — `VerifyPassword(ctx, pw) (bool, error)`
- [x] 2.9 Unit test for `classify` (not-found vs other error)

## 3. Transport sub-packages

- [x] 3.1 `httpx/` — move `JSON`/`Error` (from `respond.go`) and `SSEWriter` (from `sse.go`); stdlib only
- [x] 3.2 `auth/session.go` — `SessionStore` (token→expiry map + mutex) + cookie consts (`SessionCookieName`, `SessionDuration`)
- [x] 3.3 `auth/middleware.go` — `RequireAuth(*SessionStore, log) func(http.Handler) http.Handler`
- [x] 3.4 `auth/login.go` — `Login`/`Logout` handlers (`*SessionStore` + `*service.Service`)
- [x] 3.5 `handler/handler.go` — `Handler{ svc *service.Service }` + constructor
- [x] 3.6 `handler/{provider,agent,conversation,chat}.go` — thin handlers (decode → `h.svc.*` → `httpx`)

## 4. Slim `package api` root

- [x] 4.1 `server.go` — `Server{ svc, handlers *handler.Handler, sessions *auth.SessionStore, log }`; `NewServer(svc, log) *Server`; `ListenAndServe` / `Start`
- [x] 4.2 `routes.go` — wire via `auth.RequireAuth(s.sessions, s.log)(s.handlers.X)`, `auth.Login(s.sessions, s.svc)`, `httpx.JSON` health
- [x] 4.3 `static.go` — SPA fallback handler (split from old `routes.go`)
- [x] 4.4 `embed.go` — keep `//go:embed assets/*`

## 5. Composition root + tests

- [x] 5.1 `internal/cli/serve_cmd.go` — build `kv`/`convStore` once; `svc := service.New(mgr, kv, convStore, resolveFn, st.log)`; `api.NewServer(svc, st.log)`; `resolveFn` returns `service.AssembledAgent`
- [x] 5.2 `server_test.go` — update `setupTestServer` (no `*sql.DB` to server); qualify types (`service.*`, `auth.SessionCookieName`)
- [x] 5.3 Confirm test bodies and HTTP assertions unchanged

## 6. Verification

- [x] 6.1 `gofmt -w internal/api/...`; `go vet ./internal/api/... ./internal/cli/...`
- [x] 6.2 `go build ./...` and `make build`
- [x] 6.3 `go test ./internal/api/...` — all 7 HTTP tests pass
- [x] 6.4 `make ui` → output lands in `internal/api/assets/`
- [x] 6.5 No `database/sql` or `internal/store/sqlite` import in `internal/api/**`; `sqlite` referenced only in `serve_cmd.go`
- [x] 6.6 No raw SQL in production code: `grep -rn "QueryRowContext\|db\.Exec" internal/api/` (excl `_test.go`) is empty
- [x] 6.7 Manual smoke: `make build && ./bin/onclaw serve`; `/api/health`→200, login, `/api/providers`, SPA at `/`
