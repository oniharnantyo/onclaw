## Why

Agent add/edit today is a cramped modal (`web/src/components/Agents.tsx`) that captures only identity, LLM wiring, and a comma-separated tools string. The five resource subsystems — Tools, Hooks, Skills, MCP, Memory — each live in their own sidebar tab and are configured feature-centrically; only Tools/Hooks/Skills scope to agents, while MCP tools go to every agent and Memory is configured globally with no per-feature control. The web app also has no router, so nothing is deep-linkable. Configuring a single agent end-to-end means hopping between five tabs with no shared context.

This change makes the **Agent the integration hub**: a dedicated, routable page where an agent's tools, hooks, skills, MCP servers, memory, and persona are all configured from the agent's perspective, with global-scope resources still manageable from the same aggregate lists. It adds the per-agent stories missing today — per-agent MCP server selection and per-agent memory configuration with individual feature toggles and embedding-model selection — and gives the app real URL-based navigation.

## What Changes

- Replace the agent add/edit modal with a dedicated, tabbed **Agent page** at `/agents/:name` (and `/agents/new` for creation), served by introducing `react-router-dom`.
- Keep Hooks, Skills, Tools, MCP, and Memory as **top-level aggregate pages** (`/hooks`, `/skills`, `/tools`, `/mcp`, `/memory`) that list resources across **all scopes** (global + every agent); global-scoped hooks/skills are created and managed from within these same lists. The same section components are reused on the agent page (`/agents/:name`), pinned to that agent's scope, so each resource is also configurable per-agent. There is no global pseudo-agent.
- Make the agent-page **Tools tab per-agent**: toggling a tool there edits that agent's `agents.tools` allowlist (add/remove) via `PUT /api/agents/{name}/tools`, never the shared `tool_registry.enabled` flag. Global tool enable/disable and category config (Browser/Web) remain on the top-level `/tools` page only. The redundant comma-separated Tools field on Overview is removed in favor of these structured per-tool toggles.
- Add **per-agent MCP server selection**: a many-to-many `agent_mcp_servers` table, tool filtering at assembly (replacing the unconditional "all servers → all agents" append), and a multi-select UI. Backfilled so existing agents keep their servers.
- Add **per-agent memory configuration**: a `memory_config` JSON blob on the agent, exposing per-feature on/off toggles (core injection, extraction, retrieval, episodic, knowledge-graph, dreaming, staged-write approval, security-scan), per-agent overrides (char limit, TTLs, weights, thresholds, review model), and per-agent embedding provider/model — all composed over the global memory config.
- Add a **persona `.md` editor** so each agent's workspace persona files (`IDENTITY/SOUL/CAPABILITIES/USER/AGENTS/MEMORY.md`) are editable from the UI.
- All editable config renders **one structured field per property**, never a raw JSON textarea; persona/system-prompt textareas remain (they are free-text values).
- Turn the agent **Model Name field into a model picker combobox**: on the chosen provider it fetches live model IDs via a new `GET /api/providers/{name}/models` endpoint that reuses the CLI's `modelmeta.Enumerate` + models.dev catalog mechanism (`internal/cli/agent_models.go` `pickModel`), shows them in a dropdown with resolved metadata (context window, thinking support), and always allows typing a custom model name manually — the same "Enter custom model name manually..." fallback the CLI offers. Enumeration failures degrade gracefully to free-text.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-memory`: per-agent memory configuration, per-feature toggles, per-agent embedding model, and a per-agent security-scan switch.
- `agent-mcp`: per-agent MCP server selection and tool filtering.
- `agent-profiles`: the `agents` table stores a per-agent memory configuration; agent add/edit moves to a dedicated UI page.
- `agent-identity`: persona files become editable from the agent UI through guarded endpoints.
- `web-ui`: URL-based routing and a dedicated agent configuration page with structured config forms.

## Impact

- **Modified files**: `web/src/App.tsx` (routing shell); `web/src/components/{Agents,Hooks,Skills,Tools,MCP}.tsx` (extract scope-aware sections); `internal/cli/agent_session.go` (per-agent memory + MCP assembly); `internal/agent/agent.go` (MCP filtering); `internal/agent/middlewares/memory_middleware.go` (feature toggles); `internal/memory/{core,embedding}.go` (security-scan flag, cache keying); `internal/store/sqlite/{db,agent,memory}.go` (migrations + store methods); `internal/store/types.go` (`Agent.MemoryConfig`); `internal/api/routes.go` + `internal/api/handler/*` (new endpoints).
- **New files**: `internal/config/memory_config.go` (per-agent memory config struct + merge logic); `web/src/pages/{AgentsPage,AgentDetailPage}.tsx` and section components.
- **New dependency**: `react-router-dom@7` in `web/package.json`.
- **Schema migrations** (all guarded by existence checks): `agents.memory_config`; `agent_mcp_servers` link table (+ backfill); `embedding_cache.embedding_model` (+ composite index); `memory_documents.embedding_model`.
- **No change** to existing memory storage semantics, the provider-adapter surface, or the secrets layer.