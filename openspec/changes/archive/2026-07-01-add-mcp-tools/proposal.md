## Why

onclaw agents can only call static built-in tools (`shell`, `read_file`, `write_file`,
`list_dir`) registered at `init()` in `internal/agent/tools/`. They cannot use tools exposed
by external **MCP (Model Context Protocol)** servers — the de-facto standard way the
ecosystem ships capabilities (filesystem, git, browser, databases, custom APIs).

We want agents to discover and invoke tools from user-configured MCP servers, over the two
transports the MCP spec defines — **stdio** (local subprocess) and **Streamable HTTP**
(remote) — with legacy **SSE** support for older servers, and to manage those servers like
providers and skills: persisted in SQLite, controlled via `onclaw mcp`, hot-reloaded at
runtime.

The enabling fact: `mcpp.GetTools(ctx, &mcpp.Config{Cli: cli})` from
`eino-ext/components/tool/mcp` returns eino `InvokableTool`s for any mcp-go `*client.Client`
we hand it — stdio, streamable-HTTP, or SSE. So the agent's existing tool-injection seam
(`AssembleAgent` → `ToolsNodeConfig.Tools`) consumes MCP tools with no new abstraction, and
they inherit the existing redaction decorator (`tools.WrapRedacted`) and the agent's `tools`
allowlist filter for free.

## What Changes

- **`internal/mcp/` (new package):** a `Manager` that loads enabled `MCPServer` rows, lazily
  opens one mcp-go client per server (transport-dispatched: stdio / streamable-HTTP / legacy
  SSE), `Initialize`s each, aggregates `mcpp.GetTools` across servers, wraps every tool in
  `tools.WrapRedacted`, caches the result, and `Close()`s all clients. Per-server failure
  isolation: one bad server is logged and skipped, never breaking the agent.
- **Store:** a SQLite `MCPServerStore` (name, transport, command, args-JSON, env-JSON, url,
  enabled, timestamps) following the existing contract/types/impl 3-file pattern, mirroring
  `SkillStore`.
- **Agent wiring:** `AssembleAgent` gains one param `mcpTools []tool.BaseTool`, merged with
  builtins **before** the existing `agentConf.Tools` allowlist filter. `internal/agent` stays
  free of any MCP import.
- **CLI:** `onclaw mcp add/list/remove/test`. Transport is inferred from the flags: trailing
  args after `--` → stdio; `--url` → streamable HTTP; `--sse-url` → legacy SSE. Mutating
  commands SIGHUP the running process for hot-reload (reuse `signalRunningProcess`).
- **Hot-reload:** automatic for the CLI path (per-command re-assembly rebuilds the manager).

## Capabilities

### New Capabilities

- `agent-mcp`: configure, persist, hot-reload, and lifecycle-manage MCP tool servers
  (stdio + Streamable HTTP, legacy SSE fallback) and surface their tools to agents through
  the existing tool layer with redaction and allowlist filtering.

### Modified Capabilities

- `agent-tools`: MCP tools are merged into the agent's tool set and pass through the same
  redaction decorator and `tools` allowlist as builtins. The tool-dispatch contract is
  unchanged; MCP simply contributes additional `tool.BaseTool`s.

## Impact

- **Code:** new `internal/mcp/` package; `MCPServerStore` in
  `internal/store/{types,store}.go` + `internal/store/sqlite/{db,mcp_server,mcp_server_test}.go`;
  `internal/agent/agent.go` (one new param + append); `internal/cli/{mcp_cmd,agent_session,context,app}.go`.
- **CLI:** new `onclaw mcp ...` command group.
- **Database:** new `mcp_servers` table (idempotent migration; PK `name`).
- **Build:** add `github.com/cloudwego/eino-ext/components/tool/mcp` and
  `github.com/mark3labs/mcp-go`; `CGO_ENABLED=0` and ARM cross-compile unchanged.
- **Compatibility:** additive. Agents with no MCP servers configured behave exactly as before.