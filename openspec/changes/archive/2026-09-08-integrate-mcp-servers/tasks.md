# Tasks — integrate-mcp-servers

## 1. Dependencies & migration

- [x] 1.1 Add `github.com/cloudwego/eino-ext/components/tool/mcp` and `github.com/mark3labs/mcp-go` to go.mod; `go build ./...` green
- [x] 1.2 Write migration 000024 up/down: create `workspace_mcp_servers` and `agent_mcp_servers` (uuid PKs, workspace_id FK, agent_id FK ON DELETE CASCADE, name, transport, command, args, env JSONB, url, headers JSONB, enabled, timestamps), `ALTER TABLE agents ADD COLUMN enabled_mcps text[] NOT NULL DEFAULT '{}'`, `DROP COLUMN disabled_mcps`; down reverses
- [x] 1.3 Verify migration against the dev database (`migrate up` + `migrate status`)

## 2. Domain & stores

- [x] 2.1 Add `domain.MCPConnection` value type (transport, command, args, env rows, url, header rows) with transport-specific validation and the `stdio|streamable_http|sse` transport constants; unit tests
- [x] 2.2 Add `domain.WorkspaceMCPServer` and `domain.AgentMCPServer` entities embedding `MCPConnection` (id, workspace/agent scope, name, enabled, status, status_error, tool_count, timestamps) with name-uniqueness error sentinels
- [x] 2.3 Define `store.WorkspaceMCPServers` and `store.AgentMCPServers` ports (list/get/create/update/delete/set-status)
- [x] 2.4 Implement both ports in `store/fake` with per-scope name-uniqueness enforcement; fake tests
- [x] 2.5 Implement both ports in `store/postgres` (scan/save JSONB env/headers, cascade behavior); integration tests
- [x] 2.6 Agents rename sweep part 1 — domain + stores: `Agent.DisabledMCPs` → `Agent.EnabledMCPS`, column references `disabled_mcps` → `enabled_mcps` in postgres agents store; update store tests

## 3. MCP settings service

- [x] 3.1 Generalize the secret helpers needed by MCP out of `toolsettings.go` (encrypt/decrypt/hint/view for name-keyed secret rows) without changing tool-settings behavior; existing tests still green
- [x] 3.2 Implement `MCPSettingsService` over the two store ports: create/update validation per transport, name-keyed secret merge (empty value keeps stored secret), encryption on write, hint views on read, delete, master-switch updates; unit tests covering every workspace-mcp spec scenario
- [x] 3.3 Add agent-private support to the service (same rules, agent scope, agents-side ownership); unit tests

## 4. HTTP API

- [x] 4.1 Agents rename sweep part 2 — handlers: `disabled_mcps` → `enabled_mcps` in create/update/starter-agent payloads and responses; ignore `disabled_mcps` like a managed field; update `internal/server` handler tests
- [x] 4.2 Implement workspace MCP endpoints: `GET/POST /api/workspaces/:ws/mcp-servers`, `PATCH/DELETE /:id`, `POST /:id/probe` — `tools.read` for reads, `tools.write` for writes, 404 cross-tenant, 422 fielded validation errors; router wiring through the composition root (no nil-able deps)
- [x] 4.3 Implement agent-private MCP endpoints under `/api/workspaces/:ws/agents/:slug/mcp-servers` gated by `agents.write`; handler tests for permission and tenancy matrix
- [x] 4.4 Wire the probe to a bounded (~10s) connection attempt returning status/tool_count/error into the response and persisted row; handler tests

## 5. MCP manager & runtime

- [x] 5.1 Implement the mcp-go client factory for the three transports (stdio command+env+args; streamable HTTP and SSE url+headers) with the initialize handshake; unit tests using an in-process mcp-go server over stdio
- [x] 5.2 Implement `MCPManager`: per (workspace, server) lazy connect, cached client+tools+status, TTL idle reaper for stdio children, invalidation on update/delete/disable, `Close` teardown; unit tests for cache hit, invalidate, idle expiry
- [x] 5.3 Implement tool naming: `mcp__<server>__<tool>` sanitization, collision suffixing, over-length truncation; unit tests
- [x] 5.4 Define the `MCPPolicy` port (workspace servers for an agent's opt-in set + private servers for an agent) and implement it over the settings service; wire in the composition root
- [x] 5.5 Extend `Runner.resolve` to append MCP tools after built-ins per the agent-runtime delta; on per-server failure skip its tools and best-effort flip status to `error`; runner tests (mock policy/manager) covering opt-in, master-switch, dead-server, and allowlist-independence scenarios

## 6. Web API client & settings pane

- [x] 6.1 Add API client methods for workspace MCP CRUD/probe and agent-private CRUD; typed models replacing seed `mcpServers` reads
- [x] 6.2 Rewrite `McpPane.tsx` API-backed: cards with name/transport/status dot/tool count/used-by agents, pause-resume toggle, retry on errored, expandable read-only tool chips, delete-with-confirm, `tools.write` gating of write controls; update pane tests
- [x] 6.3 Rewrite `McpServerDialog.tsx` as the structured transport-branched form (transport select → command/args/env rows vs url/header rows; write-only secret values with stored hints); update dialog tests
- [x] 6.4 Remove MCP mock data from `seed.ts`/store (`tenant.mcpServers`, `apiAgent.mcp`) and route all reads through the API client; store tests updated

## 7. Agent config modal MCP section

- [x] 7.1 Step 3 MCP section: workspace server rows with status hint and opt-in toggles defaulting off, bound to `enabled_mcps`; paused-server warning; tests
- [x] 7.2 Agent MCP servers sub-list: private server add/edit/remove via the shared structured dialog for `agents.write` holders; hydration from agent detail; tests
- [x] 7.3 Update `CreateWorkspaceModal` starter-agent flow for the `enabled_mcps` field rename; update related tests

## 8. Transcript display names

- [x] 8.1 Add the `mcp__*` display-name branch to `toolCatalog.ts` prettifier (server + tool humanized); unit tests including sanitized and suffixed names

## 9. Verification

- [x] 9.1 `go build ./...`, `go vet ./...`, `go test ./...` all green; `go test -tags=integration ./...` green with DATABASE_URL
- [x] 9.2 Web `pnpm build` and touched vitest suites green
- [x] 9.3 Extend `scripts/smoke.sh` with MCP server CRUD + probe coverage against a stub MCP server; full smoke run green
- [ ] 9.4 Manual browser pass: register a stdio and an HTTP server, opt an agent in/out, verify tool cards in a live chat and the dead-server degradation path
