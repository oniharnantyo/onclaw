## 0. Scaffold + spec

- [x] 0.1 Create this OpenSpec change (`openspec/changes/add-mcp-tools/`)
- [x] 0.2 Add `github.com/cloudwego/eino-ext/components/tool/mcp` + `github.com/mark3labs/mcp-go`; `go mod tidy`
- [x] 0.3 `internal/mcp/` package skeleton compiling

## 1. MCPServerStore (ledger)

- [x] 1.1 `MCPServer` DTO in `internal/store/types.go` (Name, Transport, Command, Args, Env, URL, Enabled, CreatedAt, UpdatedAt)
- [x] 1.2 `MCPServerStore` interface in `internal/store/store.go` (Add/Get/List/Update/Remove)
- [x] 1.3 `mcp_servers` table migration in `internal/store/sqlite/db.go` (PK `name`; args/env as JSON TEXT)
- [x] 1.4 SQLite impl `internal/store/sqlite/mcp_server.go` mirroring `skill.go`
- [x] 1.5 Tests `internal/store/sqlite/mcp_server_test.go` (CRUD, JSON round-trip, get-missing → `sql.ErrNoRows`)

## 2. MCP manager (`internal/mcp/`)

- [x] 2.1 `client.go`: transport factory dispatching on `srv.Transport` — stdio (`NewStdioMCPClient`→Start→Initialize), http (`NewStreamableHttpClient`→Initialize), sse legacy (`NewSSEMCPClient`→Start→Initialize)
- [x] 2.2 `manager.go`: `Manager` interface (`Tools(ctx)`, `Close()`); lazy init + cache + `tools.WrapRedacted` per tool
- [x] 2.3 Failure isolation: per-server recover + init timeout; bad server logged & skipped
- [x] 2.4 `Close()` idempotent, mutex-guarded
- [x] 2.5 Tests: in-process mcp-go server (`server.NewMCPServer`) → tool surfaced; one-bad-server isolation; caching; Close idempotency

## 3. Agent wiring

- [x] 3.1 `internal/agent/agent.go`: add `mcpTools []tool.BaseTool` param; append before the allowlist filter (`agent.go:66-82`)
- [x] 3.2 Update `resolveAndAssemble` call site in `internal/cli/agent_session.go`
- [x] 3.3 Existing agent tests still pass with `nil` MCP tools

## 4. CLI assembly + lifetime

- [x] 4.1 `internal/cli/agent_session.go`: `resolveAndAssemble` takes `mcp.Manager`, calls `Tools(ctx)`, forwards to `AssembleAgent`
- [x] 4.2 `internal/cli/run.go` (and chat loop / `internal/api`): build manager once at entry, `defer Close()`
- [x] 4.3 `internal/cli/context.go`: expose `MCPServerStore` from `getProviderManager`

## 5. CLI commands

- [x] 5.1 `internal/cli/mcp_cmd.go` modeled on `skill_cmd.go`: add/list/remove/test
- [x] 5.2 Transport inference: trailing args → stdio; `--url` → http; `--sse-url` → sse; `--env KEY=VAL` (repeatable); `--disable`
- [x] 5.3 `mcp list` redacts env values
- [x] 5.4 `mcp test <name>`: open client, Initialize, list tool names
- [x] 5.5 Mutating commands call `signalRunningProcess(st.cfg.DbPath)`; register `mcpCommand` in `app.go`

## 6. Verify

- [x] 6.1 `go vet ./...`, `go test ./...` (≥80% on new Go code)
- [x] 6.2 `go build ./...`; `make build-all` (`CGO_ENABLED=0`; amd64/arm64/armv7)
- [x] 6.3 E2E: `mcp add fs -- npx -y @modelcontextprotocol/server-filesystem /tmp`; `mcp test fs`; agent invokes an MCP tool; `mcp remove` hot-reloads