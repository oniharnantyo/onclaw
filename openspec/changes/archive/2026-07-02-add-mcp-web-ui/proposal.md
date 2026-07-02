## Why

MCP tool servers can only be managed from the CLI (`onclaw mcp add/list/remove/test`, shipped by
the archived `add-mcp-tools` change). The web console — which already manages providers, agents,
skills, and hooks — has no MCP page and no `/api/mcp` endpoints, so a user of `onclaw serve` must
drop to a second terminal to configure MCP servers. Worse, the `serve` process builds a single
`mcp.Manager` that caches its discovered tool set after the first agent turn, so even CLI edits
made while the server runs never reach the live chat agent without a restart.

## What Changes

- **REST API:** a `/api/mcp` command surface (list, get, create/upsert, update, delete, toggle,
  and test) backed by the existing `MCPServerStore`, following the same service/handler/routes
  pattern as hooks. Inputs validate transport (`stdio`/`http`/`sse`) and required fields; env values
  are redacted in read responses.
- **Web UI:** a new "MCP Servers" page in the console (table + add/edit modal with
  transport-aware fields and a test-before-save action), wired into the shell's navigation.
- **Live reload:** the MCP manager gains a `Reload()` that drops its cached clients/tools so the
  next chat re-discovers from the store; mutating API endpoints invoke it, so edits made in the UI
  take effect on the running agent immediately — no restart.
- No CLI changes; the existing `onclaw mcp` commands are unchanged.

## Capabilities

### New Capabilities

<!-- None. MCP server management and the web console already exist as capabilities. -->

### Modified Capabilities

- `agent-mcp`: add web-console/API management of MCP servers (CRUD + toggle + connection test)
  alongside the existing CLI, and require that mutations hot-reload into the running agent's MCP
  tool set without a restart.
- `web-ui`: add an MCP servers management page to the console, backed by authenticated `/api/mcp`
  endpoints, consistent with the existing skills/hooks pages.

## Impact

- **Code:** `internal/mcp/manager.go` (+ test) gains `Reload()`. New `internal/api/service/mcp.go`
  and `internal/api/handler/mcp.go`; `internal/api/service/{service,types}.go`,
  `internal/api/routes.go`, `internal/cli/serve_cmd.go`, and `internal/api/server_test.go` are
  edited to wire the store + reload hook. New `web/src/components/MCP.tsx`; `web/src/App.tsx`
  edited for navigation.
- **API:** new authenticated routes under `/api/mcp` and `/api/mcp/{name}`.
- **Dependencies:** none new (reuses `mcp-go`/`eino-ext` already added by `add-mcp-tools`).
- **Database:** none (reuses the existing `mcp_servers` table).
- **Build:** web assets regenerated via `make ui` into the embedded `internal/api/assets`.
- **Compatibility:** additive. CLI users and agents with no MCP servers are unaffected.
