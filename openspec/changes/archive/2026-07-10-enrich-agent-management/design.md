# Design — enrich-agent-management

## Goals

- Make one agent fully configurable end-to-end from a single dedicated page.
- Add the missing per-agent stories: per-agent MCP server selection and per-agent memory configuration (feature toggles + embedding model).
- Give the web app real URL-based navigation and deep-linkable agent pages.
- Preserve all existing behavior for agents that have no per-agent overrides (backward compatible).

## Non-Goals

- No new memory stores or storage backends; the existing shared SQLite-backed stores remain.
- No change to the secrets layer, provider adapters, or how model metadata / reasoning are resolved (already per-agent).
- No change to how Tools/Hooks/Skills are scoped (already per-agent) — only their UI home moves.
- Not rewriting the Hooks/Skills/Tools editors — they are wrapped/relocated, not rebuilt.
- No bespoke embedding HTTP client (embeddings continue to use the eino-ext components).

## Key Decisions

### 1. Top-level menus are aggregate pages; the agent page reuses the same sections per-agent

Each `/<section>` route renders its section component **without a pinned scope**, so it lists resources across every scope (`global` + each agent) — restoring the pre-change feature-centric menus. Global-scoped hooks/skills are created and managed inside these aggregate lists (the components already support a scope selector on create). The agent page `/agents/:name` renders the **same** section components with `pinnedScope=name`, giving one end-to-end configuration surface per agent. This keeps a single component per resource (scope-aware via the existing `pinnedScope` prop) and matches the `global` vs `agent:<name>` scope model **without** a `global` pseudo-agent or a second route shape. The `pinnedScope` abstraction — `undefined` = all scopes, a concrete value = filter to that scope — is the only mechanism; the earlier `mode='global'` pseudo-agent page is removed.

### 2. Per-agent memory = middleware-level feature flags, not separate stores

The memory stores (`MemoryStore`, `EpisodicStore`, `KGStore`, etc.) are shared singletons backed by one SQLite DB and cannot be nil'd per agent. But the `MemoryMiddleware` is already constructed per agent in `AssembleAgent`. Decision: gate each middleware operation (core injection, extraction, episodic, KG, dreaming) by a per-agent feature flag, in addition to the existing store-nil gates. Retrieval is gated by registering/withholding the `memory_search`/`session_search` tools. This gives true per-feature orthogonality without duplicating storage.

### 3. `memory_config` is a JSON blob composed over the global config

The `agents` table gains one `memory_config TEXT DEFAULT '{}'` column (mirroring the existing `model_metadata` JSON-column precedent). A per-agent config parses to an `AgentMemoryConfig`; `MergeWithGlobal` resolves overrides against `config.Memory` (feature toggles AND against the relevant global enabled flag; numeric/string overrides fall back to global when zero/empty). Existing agents with an empty blob behave exactly as before. A corrupt blob logs a warning and falls back to defaults — it never blocks session start.

### 4. Per-agent embedding is safe because memories are partitioned per-agent

Memories are already queried `WHERE d.agent = ? AND (d.scope = ? OR d.scope = 'global')`, so each agent's vectors are isolated from other agents'. The remaining risk is a single agent changing its embedding model mid-life (mixed dimensions inside its own partition). Decision: key the embedding cache by `(embedding_model, content_hash)` (composite migration on `embedding_cache`), and tag each `memory_documents` row with its `embedding_model` so `SearchArchive` can filter by it. No cross-agent dimension mixing is possible because of the existing partition.

### 5. Security-scan is per-agent toggleable, default ON, with a warning

Memory writes are injected into the system prompt, so the injection/exfiltration scan is a safety guard. Decision: it is toggleable per agent (the user asked for every feature to be toggleable), but defaults ON and shows a prominent warning in the UI when disabled. Persona-file writes through the UI stay always-scanned regardless of this toggle (persona files are author-controlled prompt content).

### 6. Per-agent MCP is many-to-many with backward-compatible backfill

A new `agent_mcp_servers(agent_name, server_name, enabled)` link table associates servers to agents. At creation it is backfilled: every currently-enabled server is associated with every existing agent, so no agent loses tools. Going forward, newly-added servers are **opt-in** (not auto-associated), making the selection meaningful. MCP tools are tagged with their source server name at construction and filtered to the agent's allowed set in `resolveAndAssemble()` before reaching `AssembleAgent`.

### 7. Persona per-workspace already satisfies per-agent identity; this adds only a UI editor

The `agent-identity` spec already places persona files in each agent's workspace (`~/.onclaw/workspace/<name>/`), seeded from embedded templates. Decision: no storage work — only guarded HTTP endpoints (`GET/PUT /api/agents/{name}/persona/{file}`) that whitelist filenames, apply `filepath.Base()`, resolve against the agent's DB-sourced workspace, and scan writes, plus a file-list/textarea editor in the page.

### 8. `react-router-dom` sets the app-wide navigation precedent

The app is currently a conditional-render SPA with no URLs. Introducing `react-router-dom@7` (React 19 compatible) and migrating `App.tsx` to `<Routes>` sets the pattern every future tab follows. The sidebar becomes `<Link>`s; `/` redirects to `/chat`. This is a mechanical shell refactor kept in its own phase so it can be verified to preserve all existing screens before any new UI lands.

### 9. Agent-page tabs scroll via a bounded flex host

`.main-content` is `height:100vh; overflow:hidden`, so each route owns its scroll. The agent page's `.tab-content` is a bounded flex host (`flex-grow:1; min-height:0; display:flex; flex-direction:column; overflow:hidden`) so an embedded section page (`flex-grow:1`) receives a bounded height and scrolls internally via its own `overflow:auto` content div — single scrollbar, no double padding. Directly-rendered tab forms (Overview, Memory) use a `.tab-pane` wrapper (`overflow-y:auto; padding:20px 24px`) for the same effect. The `.tab-container` strip is inset 24px to align with the toolbar and clear the sidebar.

### 10. Per-agent tool selection edits the CSV allowlist, not the global registry

Tools have two independent flags: `tool_registry.enabled` (global availability — disabled means no agent gets the tool) and `agents.tools` (a per-agent CSV allowlist applied at assembly: empty = all enabled tools, non-empty = that subset). The agent-page Tools tab must toggle the **per-agent** allowlist, not the global flag — otherwise disabling a tool for one agent removes it for everyone (the bug this corrects; the `Tools.tsx` `pinnedScope` prop was declared but never used). Unlike per-agent MCP (decision #6, a junction table with per-row upserts), tool selection reuses the existing `agents.tools` CSV column, so the endpoint is a **whole-list replace** (`PUT /api/agents/{name}/tools` body `{tools}`) with the frontend computing the next list (the empty = all semantic is handled client-side). `Tools.tsx` switches on a `variant: 'global' | 'agent'` prop (replacing the unused `pinnedScope`): global mode toggles `tool_registry.enabled` + category config (top-level `/tools`); agent mode toggles allowlist membership (edit → PUT; create → local form state), hiding global-only Config controls. Known footgun preserved: an empty `agents.tools` means all tools, so clearing the list re-enables everything.

### 11. Model picker reuses the CLI's `modelmeta` mechanism behind a provider-models endpoint

The CLI already discovers models via `internal/modelmeta.Enumerate(ctx, providerType, apiBase, apiKey)` (live, per-provider) enriched with the cached models.dev catalog (`modelmeta.GetCatalog`) and per-model metadata (`modelmeta.Resolve`) — see `internal/cli/agent_models.go` `pickModel`. The web UI reuses this exact mechanism (no second model-listing implementation) behind a new `GET /api/providers/{name}/models` endpoint: resolve the provider profile + secret (as the CLI does — `GetProfile` + `ResolveSecret`), seed the context with `modelmeta.OpenaiModelsCacheKey` + `ModelCache` to avoid N+1 discovery, run `Enumerate`, and return `{ models: [{ id, contextWindow, thinking, inputModalities }], warning? }`. The agent Model Name field becomes a combobox (an `<input>` + `<datalist>`): pick an enumerated model or type any custom name, repopulating when the provider changes. If enumeration fails or returns nothing, the endpoint returns `200` with `{models:[], warning}` and the field degrades to free-text with the warning shown — mirroring the CLI's `WARNING: failed to enumerate models... Please enter model name manually.` `internal/modelmeta` remains the single source of truth shared by CLI and web.