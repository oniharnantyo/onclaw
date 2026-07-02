# Tasks

## 1. Manager live-reload (`internal/mcp/`)

- [x] 1.1 Add `Reload()` to the `Manager` interface and the `manager` impl; extract the shared
  reset logic (close + drop clients, clear cache) from `Close()` so `Reload()` reuses it
- [x] 1.2 `Reload()` takes `mu`, swallows/logs close errors (best-effort), leaves `loaded=false` so
  the next `Tools()` re-reads the store
- [x] 1.3 Extend `internal/mcp/manager_test.go`: cache serves stale tools until `Reload()`, then a
  newly-added server's tool appears; reload does not panic on empty state

## 2. API service layer (`internal/api/service/`)

- [x] 2.1 Add `mcpStore store.MCPServerStore` and `reloadMCP func()` fields to `Service`; extend
  `New(...)` (nil-guard `reloadMCP`)
- [x] 2.2 Create `mcp.go`: `ListMCP`, `GetMCP`, `AddMCP` (upsert add-or-update), `UpdateMCP`,
  `RemoveMCP`, `ToggleMCPServer` (Get → flip `Enabled` → `UpdateServer`), `TestMCP`
- [x] 2.3 Add transport/field validation in `AddMCP`/`UpdateMCP` (transport ∈ {stdio,http,sse};
  stdio ⇒ command; http/sse ⇒ url; valid JSON args/env)
- [x] 2.4 Mutating methods call `s.reloadMCP()` after a successful store write
- [x] 2.5 `TestMCP` connects via `mcp.NewClient` + `ListTools` and returns discovered tool names
- [x] 2.6 Add `MCPServerView` to `types.go` (env values redacted, keys preserved); use it in
  `ListMCP`/`GetMCP`

## 3. API handlers + routes (`internal/api/`)

- [x] 3.1 Create `handler/mcp.go` mirroring `handler/hooks.go` (decode → `svc` → `httpx.JSON`)
- [x] 3.2 Register `/api/mcp`, `/api/mcp/{name}`, `/api/mcp/{name}/toggle`, `/api/mcp/test` under
  `requireAuth` in `routes.go`
- [x] 3.3 Wire `mcpStore` and `mcpMgr.Reload` into `service.New(...)` in `serve_cmd.go`
- [x] 3.4 Update `server_test.go` constructor call to match the new `New(...)` signature

## 4. Tests (`internal/api/`)

- [x] 4.1 CRUD happy paths (create/upsert, get, update, remove) over the API
- [x] 4.2 Validation rejections (bad transport, stdio missing command, http missing url) → 400
- [x] 4.3 Toggle flips `Enabled`; mutating calls trigger reload (assert via a fake reload callback)
- [x] 4.4 List/get responses redact env values
- [x] 4.5 `POST /api/mcp/test` returns tool names (in-process mcp-go server) and persists nothing

## 5. Web UI (`web/src/`)

- [x] 5.1 Create `components/MCP.tsx` (table + add/edit modal) following MASTER.md; transport-aware
  fields (stdio: command/args/env; http/sse: url) + Test panel calling `/api/mcp/test`
- [x] 5.2 Wire into `App.tsx`: add `'mcp'` to `Tab`, nav item under "Agent", `HEADER_TITLES` entry,
  `loadMCPServers` loader + state, conditional render
- [x] 5.3 `npm run lint` / typecheck clean in `web/`

## 6. Build + verify

- [x] 6.1 `make build` (static, `CGO_ENABLED=0`); `go vet ./internal/mcp/... ./internal/api/... ./internal/cli/...`
- [x] 6.2 `go test ./internal/mcp/... ./internal/api/...` green
- [x] 6.3 `make ui` regenerates embedded `internal/api/assets`
- [x] 6.4 E2E: `onclaw serve` → add a stdio server in the console → Test shows tools → toggle →
  next chat turn invokes an MCP tool (proves live reload)
