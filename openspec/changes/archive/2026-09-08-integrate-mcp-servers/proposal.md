# Proposal: integrate-mcp-servers

## Why

MCP is mock everywhere today: the settings pane renders fabricated seed data, the `agents.disabled_mcps` column is persisted but read by nothing, and no MCP client exists in the backend. Agents therefore have no way to reach the MCP ecosystem — the fastest path to real integrations (GitHub, Postgres, Slack, …) without writing each as an OnClaw tool.

## What Changes

- Add a real MCP layer: workspace admins register MCP servers (stdio, Streamable HTTP, or SSE transports) per workspace; agents subscribe to them **opt-in**; agents can also carry **private** MCP servers visible only to themselves.
- New workspace MCP registry: CRUD API, connection config with encrypted secrets (env vars, headers), enable/disable master switch, connect-and-probe status with tool counts.
- **BREAKING** — `agents.disabled_mcps` (dead denylist) is replaced by `agents.enabled_mcps`, an opt-in allowlist of workspace MCP server ids; agents get no workspace MCP until they subscribe.
- Agent config gains per-agent workspace-server toggles (server-level granularity) and an agent-private server sub-list with the same structured config form.
- Runtime: an MCP connection manager serves each run with tools from the agent's opted-in workspace servers plus its private servers; tools surface as `mcp__<server>__<tool>`; a dead server skips its tools and flips to error status — it never fails the run.
- Web UI: the mock MCP pane and add-server dialog become real (structured transport-branched config form, status dots, tool counts, used-by counts); transcript tool cards resolve `mcp__*` display names.

## Capabilities

### New Capabilities

- `workspace-mcp`: MCP server registry per workspace and per agent — registration API, transports, secret handling, agent opt-in selection, agent-private servers, probing/status, and the settings + agent-config UI requirements.

### Modified Capabilities

- `agents`: the agent schema's `disabled_mcps` denylist is replaced by the `enabled_mcps` opt-in allowlist (create defaults, update semantics, and inert-name handling change accordingly).
- `agent-runtime`: tool selection gains an MCP resolution requirement — how opted-in workspace servers and agent-private servers contribute tools at execution time, how names surface, and how failures degrade.
- `web-app/settings`: the MCP servers pane requirement is rewritten from the mock (name + free-text transport string) to API-backed CRUD with a structured transport-branched config dialog, probe status, and write-permission gating.
- `web-app/agents`: the structured agent configuration's Step 3 gains the MCP section — workspace server opt-in toggles defaulting off plus the agent-private server sub-list.

## Impact

- **Backend**: new `internal/agents/tools/mcp` connection manager + eino-ext MCP component (`github.com/cloudwego/eino-ext/components/tool/mcp`, `mcp-go` clients); new store port + postgres/fake impls for two tables (`workspace_mcp_servers`, `agent_mcp_servers`); migration adding both tables, adding `agents.enabled_mcps`, dropping `agents.disabled_mcps`; agent domain/store/handlers rename sweep; new workspace-scoped MCP handlers; runner tool resolution extension.
- **Frontend**: real `McpPane` + `McpServerDialog` (structured transport-branched form), `AgentConfigModal` MCP section (workspace toggles + private sub-list), API client methods, `toolCatalog` display-name fallback for `mcp__*` keys.
- **APIs**: new `/api/workspaces/{slug}/mcp-servers` endpoints (create/list/patch/delete/probe) gated by `tools.write`; agent create/update payloads change `disabled_mcps` → `enabled_mcps` (**BREAKING**); agent-private servers managed under the agent edit permission.
- **Security**: MCP credentials encrypt with the existing workspace-AAD scheme and are write-only over the API (last-4 hints); stdio configuration requires Owner/Admin since it executes a command on the server host.
- **Out of scope (v1)**: OAuth flows, per-tool selection within a server, background health loops, MCP resources/prompts/sampling.
