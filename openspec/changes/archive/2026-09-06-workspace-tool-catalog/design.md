# Design: workspace-tool-catalog

## Context

`enrich-agent-tools` (implemented, un-archived) left the runtime with a `ToolRegistry` of six registry tools plus the reserved `execute`, eino filesystem middleware tools attached unconditionally, an env-configured search provider chosen once at registry construction, and a go-rod browser manager with env-only configuration. The frontend hardcodes its tool list and its icon map keys match nothing. This change introduces the missing catalog + workspace layer without reworking the runtime seam.

## Goals / Non-goals

- Goals: one backend catalog feeding all surfaces; workspace-level gate and per-tool config; readable iconed chips (flat, per user decision: **no group headings**); Browser and Shell as single chips; browser tool enrichment after BrowserMCP's snapshot+ref model; per-workspace search provider/credential; browser runtime config.
- Non-goals: OS-level sandboxing of shell (unchanged trust statement); MCP backend (stays client-side mock); skills API backend gap (explicitly skipped by the user); reworking the provider-key model (reused as-is).

## Decisions

### D1 · Catalog lives beside the registry, metadata-only

`internal/agents/tool_registry.go` gains a `ToolCatalog` (metadata records) separate from the constructor registry — the fs middleware tools and `execute` have no registry constructors but must appear. Record: `{Key, DisplayName, Description, Group, IconKey, Configurable *ConfigSchema}`. `ConfigSchema` is `[]ConfigField` with `{Key, Label, Type (secret|text|number|boolean|enum), Required, Help, Options}` — the settings dialog renders generically from this (plugin-first: new configurable tool ⇒ no frontend edit). Keys are allowlist names: `browser` is the facade alias; members keep their dotted names.

The catalog is assembled in the `agents` package and served by a new handler — the HTTP layer must not duplicate metadata.

### D2 · Browser facade = allowlist alias expanded at resolution

`ResolvedTools` expands the `browser` alias to the current full browser set before filtering. Individual `browser.*` names keep resolving (backward compat). The edit UI shows Browser selected when the allowlist contains the alias **or** any `browser.*` name and normalizes to the alias on save. Rationale for alias-over-expanded-names: future browser tools auto-grant to agents that opted into Browser, matching the user's "treat browser as facade" framing. Tool-call transcript cards keep showing the concrete `browser.click` etc.

### D3 · Filesystem tools join the allowlist via `ToolConfig.Disable`

eino's `filesystem.ToolConfig` has a `Disable` field (verified at eino v0.10.0-alpha.28), so no fork: the runner maps allowlist membership to per-tool `Disable` flags in its `FilesystemConfig`. Behavior change: fs tools were unconditional. Mitigation in migration 000019 (below): existing agents with empty `tools` are seeded with the six fs names so nobody loses capability; fresh agents that skip Step 3 intentionally get none (matches the existing "capabilities are deliberate" wizard default).

### D4 · Workspace gate injected as a narrow resolver

Per the DI rules (granular deps, functional options), the `Runner` gains `WithToolPolicy(ToolPolicy)` where `ToolPolicy` is a narrow interface: `EnabledTools(ctx, workspaceID) (map[string]bool, error)` returning the effective enabled set (defaults applied when no rows). `resolve()` intersects: allowlist → expand `browser` alias → filter by policy. Per-turn `AllowedTools` filtering happens after the policy intersection so the gate always wins. The composition root wires the postgres-backed resolver; the in-memory fake serves tests. No `if policy != nil` guards.

### D5 · ToolContext carries resolved workspace config

`ToolContext` (already per-execution, already carries `WorkspaceSlug`) gains `ToolConfigs map[string]map[string]any` — the resolved settings for configurable tools (secrets decrypted, merged with env fallbacks). The search provider is constructed **per execution** from `ToolConfigs["web.search"]` instead of once at registry construction; `BrowserManager` reads `ToolConfigs["browser"]`. This is the interface fix that removes the env-from-registry-construction wart. Env fallback resolution lives in the policy resolver, not in the tools.

### D6 · Secrets reuse the provider-key pattern

`workspace_tool_settings.config` jsonb holds secret fields encrypted via `secrets.Encrypt(encryptionKey, workspaceID, value)` before marshal (same key derivation as `workspace_providers.key_ciphertext`). GET responses decrypt nothing — they emit non-secret values verbatim and `hint` (last 4) for secrets. PATCH writes new ciphertext only when the client supplies a non-empty secret field. The store keeps this in one place so handlers stay thin.

### D7 · Search provider registry

`SearchProvider` interface exists (`websearch_providers.go`); it gains a registration table `{id → {label, credential kind, constructor(credential)}}`. First cut implements: `duckduckgo` (exists), `tavily` (exists), `brave`, `exa`, `perplexity`, `firecrawl` (plain REST/JSON clients — each is a thin request/parse pair modeled on tavily), `searxng` (base URL + `/search?format=json`). The catalog's `web.search` config schema: `provider` (enum from the registry) + `api_key` (secret) or `base_url` (text) — requiredness follows the chosen provider's credential kind.

### D8 · Browser enrichment after BrowserMCP, with one spike

New tools: `browser.snapshot` (accessibility/DOM snapshot with element refs), `browser.click`, `browser.type`, `browser.hover`, `browser.drag`, `browser.select_option` — ref-targeted, each returning a fresh snapshot. `browser.act` stays (scroll et al). Feasibility note: go-rod exposes raw CDP, so the snapshot is expected via `DOMSnapshot.captureSnapshot`; this is the one genuinely unknown piece — **spike task first**, with the ref format (backend node-id based) decided from the spike. Per-session `max_pages` caps open tabs; `action_timeout_seconds` wraps each tool call in a rod context timeout; `idle_timeout_seconds` is enforced by the session manager's GC (session closes after inactivity). `headless` ignored when `remote_cdp_url` is set (validated server-side as a no-op, explained in UI help text).

### D9 · Settings storage + API

Table `workspace_tool_settings(workspace_id fk, tool_key text, enabled bool not null default true, config jsonb not null default '{}', updated_at, primary key (workspace_id, tool_key))` — up/down pair 000019. Migration also seeds: `UPDATE agents SET tools = array_append(tools, …)` for agents whose `tools = '{}'` → `{ls, read_file, write_file, edit_file, glob, grep}`. API: `GET /api/v1/workspaces/:ws/tools` (merge catalog × settings), `PATCH /api/v1/workspaces/:ws/tools/:key`. Permission: new `tools.write` in the domain permission catalog granted to Owner/Admin; read covered by membership (mirrors providers pane). Enabling an unconfigured configurable tool → 422 naming missing fields (server validates against `ConfigSchema` + provider credential kind).

### D10 · Frontend: two surfaces, one source

- **Agent dialog** (`AgentConfigModal.tsx`): replace the hardcoded `TOOLS` + dead `TOOL_ICON` map with `api.tools.list(wsId)` fed into the existing `OptionChips` (`iconOf` already exists). Flat chips, no headings. Browser/Shell normalization per D2. Workspace-disabled chips: greyed, lock glyph, tooltip "Disabled in Settings → Tools" — data arrives in the same response (`enabled: false`), no extra call.
- **Settings** (`SettingsPage.tsx`): add `{ id: 'tools', label: 'Tools', icon: 'zap' }` to `SETTINGS_SECTIONS` (after Skills) and a new `ToolsPane.tsx` following the ProvidersPane pattern (API-backed, toggle + toast guard handling). Gear button on configurable tools opens the existing `Modal` rendering `ConfigField[]` generically (secret = write-only password input with hint; number/text/bool/enum per type). The pane is a flat list — description column carries the group semantics instead of headings, per user decision.
- **Icons** (`Icon.tsx`): add `folder`, `file-plus`, `edit`, `scan`, `compass` (existing `search`, `globe`, `link`, `terminal`, `file` reused). Icon keys travel from the catalog.

## Risks / Trade-offs

- **Alias magic vs explicitness**: the `browser` alias hides member tools from the raw allowlist. Accepted — it is exactly the facade semantics the user asked for, and legacy names keep resolving.
- **Per-execution provider construction** (D5) trades a tiny setup cost for correctness (per-workspace credentials); the DuckDuckGo scraper construction stays cheap.
- **CDP snapshot spike** (D8) is the only feasibility unknown; if `DOMSnapshot` proves unusable, the fallback is rod's element-query-based refs, still ref-targeted.
- **Migration seeding** (D3) touches agent rows; the down migration must not strip the seeded names from agents where users added tools afterwards — down simply drops the table; the seeded allowlist entries are inert keys once the fs tools stop being catalog-gated (they resolve as unknown names → inert), acceptable for a rollback path.

## Migration plan

000019 up: create `workspace_tool_settings`; seed fs names into empty agent allowlists. 000019 down: drop table (agent `tools` untouched — see D6/D9 risk note). No data backfill needed for tool settings (absence = enabled default).

## Open questions

- None blocking. (User resolved: browser = facade; "ide timeout" = idle timeout; configurable tools start disabled until configured; skills backend gap out of scope.)
