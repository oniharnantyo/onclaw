# Proposal: workspace-tool-catalog

## Why

There is no tool catalog: the backend registry stores only name → constructor, no endpoint exposes it, and the agent form hardcodes a stale 7-chip list (`constants.ts`) that is missing the six filesystem tools (`ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`), renders no icons (the `TOOL_ICON` map keys match no tool id), and splits the browser across four chips. There is also no workspace-level control over tools: nothing workspace-scoped gates what agents may use, `web.search` is locked to one instance-wide provider chosen by env (`ONCLAW_SEARCH_PROVIDER` / `ONCLAW_TAVILY_API_KEY`), and the browser is env-configured (`ONCLAW_BROWSER_CDP_URL` only; headless hardcoded, one page per session, no timeouts).

## What Changes

- **Tool catalog (backend, single source of truth).** Registry entries plus the filesystem middleware tools and the reserved shell name gain catalog metadata: display name, description, group, icon key, configurability. New endpoint `GET /api/v1/workspaces/:ws/tools` returns the catalog merged with workspace state. The frontend agent form replaces its hardcoded list with this endpoint — a newly registered tool appears in the UI without frontend edits.
- **Human-readable agent form chips, flat.** Step 3 / Capabilities tab shows one flat row of iconed chips using catalog display names (List Files, Read File, Write File, Edit File, Glob, Grep, Web Search, Web Fetch, Browser, Shell); tool ids stored in `agent.tools` are unchanged. No group headings. Shell is one chip (`execute`), and the browser facade is one chip: the new alias `browser` in an agent's allowlist expands to the full browser tool set at resolution.
- **Filesystem tools become selectable.** The six fs middleware tools join the allowlist, enforced via eino's `filesystem.ToolConfig.Disable`. Migration seeds existing agents' empty `tools` with the six fs names so no existing agent loses file capability; fresh agents that skip Step 3 get none (unchanged wizard semantics).
- **Browser tool enrichment.** The browser set grows from 4 to 10 under the facade: existing `browser.navigate/act/read/screenshot` plus ref-targeted `browser.snapshot`, `browser.click`, `browser.type`, `browser.hover`, `browser.drag`, `browser.select_option` (accessibility-snapshot + element-ref model after BrowserMCP/Playwright MCP).
- **Workspace Tools settings pane (API-backed).** New `Tools` section in workspace settings: flat list of all catalog tools with readable names, descriptions, and an enable toggle acting as the global tool status; configurable tools get a gear button opening a structured config dialog. Backed by a new `workspace_tool_settings` table (per-tool `enabled` + `config jsonb`), `PATCH /api/v1/workspaces/:ws/tools/:key`, and the existing providers key-handling pattern for secrets (write-only, ciphertext at rest, hint-only reads).
- **Workspace gate at resolution.** A workspace-disabled tool is removed from every agent's effective surface in that workspace — winning over the agent allowlist and the per-turn `AllowedTools` override. The agent form shows workspace-disabled tools greyed and unselectable.
- **web.search per workspace.** Provider registry (`duckduckgo` key-free, `tavily`, `brave`, `exa`, `perplexity`, `firecrawl` API-key, `searxng` base-url) with per-workspace provider + credential from tool settings; instance env becomes the fallback. `web.search` starts disabled until a provider is configured (choosing key-free DuckDuckGo counts as configured).
- **Browser per workspace.** Headless toggle, remote CDP URL, max pages, idle timeout, action timeout move from env/hardcode into tool settings and reach the browser manager through the tool context.

## Capabilities

### New Capabilities

- `workspace-tools`: workspace tool settings storage, catalog + settings API, resolution gating, search-provider and browser configuration.

### Modified Capabilities

- `agent-runtime`: tool selection gains fs middleware tool names, the `browser` facade alias, and the workspace gate; web search provider selection moves to per-workspace settings; browser set enriched and workspace-configured.
- `web-app/agents`: Step 3 / Capabilities tools UI — flat, iconed, readable names, Browser/Shell single chips, workspace-disabled state.
- `web-app/settings`: settings navigation gains the Tools section (nine sections); new Tools pane requirement.

## Impact

- **Depends on:** stacks on the un-archived `enrich-agent-tools` change (allowlist model, shell/fetch/search/browser tools). Its "Tool selection" requirement is restated here; archive order matters (enrich first).
- **Breaking-ish (mitigated):** fs tools become allowlist-gated; migration 000019 seeds existing agents so behavior is preserved. The browser facade is additive — legacy agents carrying individual `browser.*` names keep working; the edit UI normalizes them to the alias on save.
- **New table:** `workspace_tool_settings` (workspace scoped, one row per tool key; down migration drops it).
- **New endpoint surface:** `GET /workspaces/:ws/tools`, `PATCH /workspaces/:ws/tools/:key` (settings-management permission; Owner/Admin).
- **Env deprecation (soft):** `ONCLAW_SEARCH_PROVIDER` / `ONCLAW_TAVILY_API_KEY` / `ONCLAW_BROWSER_CDP_URL` remain as fallbacks when a workspace has no explicit settings.
- **UI contract:** flat chips (no grouping), readable names with icons, Browser/Shell single chips in the agent dialog; tools list with toggles and gear dialogs in settings — as agreed with the user.
