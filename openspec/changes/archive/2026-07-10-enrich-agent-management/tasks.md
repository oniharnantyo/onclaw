# Tasks

## 1. Routing foundation

- [x] 1.1 Add `react-router-dom@7` to `web/package.json`.
- [x] 1.2 Create `web/src/pages/`; migrate `web/src/App.tsx` from the `activeTab` conditional block to `<BrowserRouter>` + `<Routes>`.
- [x] 1.3 Extract `ChatPage`, `ProvidersPage`, `AgentsPage` wrappers; convert sidebar items to `<Link>`; redirect `/` → `/chat`.
- [x] 1.4 Verify every existing screen is reachable by URL and browser back/forward works; `npm run build` passes.

## 2. Agent page shell

- [x] 2.1 Create `web/src/pages/AgentDetailPage.tsx` with `mode: 'create' | 'edit'`, tabbed sections (Overview, Hooks, Skills, Memory, MCP, Tools, Persona), back-link, loading/error states.
- [x] 2.2 Wire routes `/agents`, `/agents/new`, `/agents/:name` (drop the `/agents/global/:section` pseudo-agent route).
- [x] 2.3 Move the Overview fields (name/provider/model/reasoning/system-prompt/max-iterations/default) out of `Agents.tsx` modal into the page; remove the modal.
- [x] 2.4 Verify create + edit replace the modal; agent-page sections render scoped to the named agent.
- [x] 2.5 Make `.tab-content` a bounded flex host and add a `.tab-pane` wrapper for the Overview/Memory forms so agent-page tabs scroll and inset 24px from the sidebar.

## 3. Relocate Hooks/Skills/Tools

- [x] 3.1 Extract scope-aware versions of `Hooks.tsx`, `Skills.tsx`, `Tools.tsx` that accept a pinned scope (agent name or `global`). *(Tools scope-awareness is realized in §8 — its `pinnedScope` was previously declared but unused.)*
- [x] 3.2 Render Hooks/Skills/Tools as agent-page sections pinned to the agent; keep the top-level sidebar entries at their aggregate `/{section}` pages (which list all scopes).
- [x] 3.3 Verify CRUD works identically and scope filtering is correct.

## 4. Per-agent MCP selection

- [x] 4.1 Add guarded migration for `agent_mcp_servers` table + backfill (all enabled servers → all existing agents) in `internal/store/sqlite/db.go`.
- [x] 4.2 Add store methods `ListServersForAgent` / `SetAgentServers` (in `internal/store/sqlite/agent.go` or a new `agent_mcp.go`).
- [x] 4.3 Tag each MCP tool with its source server name at construction (inline in `agent.go`/`agent_session.go`).
- [x] 4.4 Filter MCP tools to the agent's allowed set in `resolveAndAssemble()` before `AssembleAgent`.
- [x] 4.5 Add `GET/PUT /api/agents/{name}/mcp-servers` handlers + routes (`internal/api/routes.go`, `internal/api/handler/*`).
- [x] 4.6 Add the `AgentMCPSection` multi-select UI.
- [x] 4.7 Verify backfill keeps existing servers; deselecting a server removes only its tools for that agent.

## 5. Per-agent memory configuration

- [x] 5.1 Create `internal/config/memory_config.go`: `AgentMemoryConfig` struct (feature toggles + overrides + embedding), `DefaultAgentMemoryConfig()`, `MergeWithGlobal(global) *MergedMemoryConfig`.
- [x] 5.2 Guarded migration: add `agents.memory_config TEXT NOT NULL DEFAULT '{}'`; add `MemoryConfig string` to `Agent` (`internal/store/types.go`); read/write in `internal/store/sqlite/agent.go`.
- [x] 5.3 In `resolveAndAssemble()`: parse `agentConf.MemoryConfig` (warn+default on error), merge with global, build the embedder per-agent from merged embedding config.
- [x] 5.4 Extend `internal/agent/middlewares/memory_middleware.go` to accept feature flags and gate `BeforeAgent` core injection and each `FlushMessages` operation (extraction/episodic/KG/dream); gate `memory_search`/`session_search` tool registration on `retrieval`.
- [x] 5.5 Thread a `securityScan` flag into `internal/memory/core.go` `CoreStore` (constructor + `WriteCore`) and entity extraction; default ON.
- [x] 5.6 Guarded migration: add `embedding_cache.embedding_model` + composite index; add `memory_documents.embedding_model`; key cache by `(model, content_hash)` in `internal/memory/embedding.go`; filter `SearchArchive` by model in `internal/store/sqlite/memory.go`.
- [x] 5.7 Add `GET/PUT /api/agents/{name}/memory-config` handlers + routes.
- [x] 5.8 Define a JSON Schema for the memory config form; build `AgentMemorySection` with structured fields (toggles, number/text inputs, embedding select) following the Browser-config pattern; add a warning banner when `security_scan` is off.
- [x] 5.9 Verify each toggle independently disables its operation; zero-value overrides fall back to global; per-agent embedding model works without cache collision; agents with an empty blob behave as before.

## 6. Persona file editor

- [x] 6.1 Add `GET /api/agents/{name}/persona`, `GET/PUT /api/agents/{name}/persona/{file}` handlers + routes; whitelist filenames, apply `filepath.Base()`, resolve against the DB-sourced workspace, always run `ScanContent` on writes.
- [x] 6.2 Build `AgentPersonaSection` (file list + textarea editor + save).
- [x] 6.3 Verify list/read/write each file; path traversal blocked; edits appear in the agent's next session prompt.

## 7. Polish, tests, specs

- [x] 7.1 Add tests for `AgentMemoryConfig.MergeWithGlobal`, MCP filtering, persona endpoint validation, and the memory-middleware toggles (black-box, ≥ 70% coverage per touched package).
- [x] 7.2 Web E2E: (N/A — no web test runner/harness is present in repository; manually verified flows).
- [x] 7.3 Update `CLAUDE.md` with the routing convention and the agent-page structure.
- [x] 7.4 `make build && make vet && make test`; `cd web && npm run build && npm run lint`.

## 8. Per-agent tool selection

- [x] 8.1 Add `UpdateAgentTools(ctx, name, tools string)` to the `AgentStore` interface (`internal/store/store.go`) + sqlite impl (`UPDATE agents SET tools = ?, updated_at = ? WHERE name = ?`, rows-affected → `sql.ErrNoRows`).
- [x] 8.2 Add `SetAgentTools` service method + `SetAgentTools` handler + `PUT /api/agents/{name}/tools` route (`internal/api/routes.go`), mirroring the per-agent MCP handlers (`SetAgentMCP`).
- [x] 8.3 Wire `web/src/components/Tools.tsx` to a `variant: 'global' | 'agent'` prop (replacing the unused `pinnedScope`): agent mode toggles membership in `agents.tools` (edit → `PUT /api/agents/{name}/tools; create → local form state) and hides global-only Config buttons (Browser category / Web per-tool); global mode (`/tools` page) unchanged.
- [x] 8.4 Drop the comma-separated Tools field on the Overview tab (`web/src/pages/AgentDetailPage.tsx`); create-mode Tools tab edits local form state saved on create.
- [x] 8.5 Tests (handler/service/store, ≥70% coverage per touched package) + verify toggling a tool on the agent page leaves `tool_registry.enabled`, the `/tools` page, and other agents unchanged.

## 9. Model picker (agent Model Name field)

- [x] 9.1 Add `GET /api/providers/{name}/models` handler + service + route: resolve provider (`GetProfile` + `ResolveSecret`), seed ctx with `modelmeta.OpenaiModelsCacheKey` + `ModelCache`, call `modelmeta.Enumerate(ctx, providerType, apiBase, apiKey)` with `modelmeta.GetCatalog()`; return `{models:[{id, contextWindow, thinking, inputModalities}]}`; on enumerate error/empty return `200` with `{models:[], warning}` (mirror `pickModel` in `internal/cli/agent_models.go`).
- [x] 9.2 Convert the agent Model Name field (`web/src/pages/AgentDetailPage.tsx`) to a combobox (`<input>` + `<datalist>`): on provider change, fetch `/api/providers/{name}/models`; list model IDs (with context/thinking hints); always allow free-text custom entry; show the warning + free-text-only when enumeration fails/empty.
- [x] 9.3 Tests (handler/service, ≥70% coverage per touched package) — enumerate success, enumerate failure (graceful `{models:[], warning}`), unknown provider; `cd web && npm run build && npm run lint`.