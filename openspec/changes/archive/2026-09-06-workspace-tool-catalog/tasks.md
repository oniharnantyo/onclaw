# Tasks: workspace-tool-catalog

## 1. Storage & domain

- [x] 1.1 Migration `000019_workspace_tool_settings` (up: table + seed fs tool names into agents with empty `tools`; down: drop table)
- [x] 1.2 Domain: `WorkspaceToolSetting` type + validation (tool key known to catalog, config values per schema, positive numbers for max_pages/timeouts); `tools.write` permission added to the catalog, granted to Owner/Admin
- [x] 1.3 Store port `store.ToolSettingsStore` (Get, Upsert, List by workspace) + in-memory fake
- [x] 1.4 Postgres adapter for the port (workspace-scoped queries, jsonb config round-trip)
- [x] 1.5 Secrets: encrypt-on-write / hint-on-read helpers for secret config fields, keyed like workspace provider keys (unit tests for round-trip + hint)

## 2. Tool catalog

- [x] 2.1 `ToolCatalog` metadata records in `internal/agents`: all registry tools, six fs tools, `execute`, `browser` facade alias (display names: List Files, Read File, Write File, Edit File, Glob, Grep, Web Search, Web Fetch, Browser, Shell); groups + icon keys
- [x] 2.2 Config schemas: `web.search` (provider enum + credential field per kind), `browser` (headless, remote_cdp_url, max_pages, idle_timeout_seconds, action_timeout_seconds with defaults + help text)
- [x] 2.3 Search provider registry table (id → label, credential kind, constructor); register duckduckgo, tavily (existing), brave, exa, perplexity, firecrawl, searxng (new thin clients)
- [x] 2.4 `GET /api/v1/workspaces/:ws/tools` handler: catalog merged with settings (enabled, configured, config view with secret hints); route + membership read permission
- [x] 2.5 `PATCH /api/v1/workspaces/:ws/tools/:key` handler: validation (unknown key 400, invalid values 422 naming fields, enable-without-config 422, member 403), settings persistence; route wiring

## 3. Runtime gating & config plumbing

- [x] 3.1 `ToolPolicy` narrow interface + `WithToolPolicy` runner option; composition root wires the postgres resolver; fake for tests
- [x] 3.2 `resolve()` intersection: allowlist → expand `browser` alias → workspace-gate filter → per-turn `AllowedTools` filter (gate wins)
- [x] 3.3 fs tools gating: map allowlist names to eino `ToolConfig.Disable` in `FilesystemConfig`; empty allowlist disables all six
- [x] 3.4 `ToolContext.ToolConfigs` (resolved, secrets decrypted, env fallback merged); per-execution search provider construction from `web.search` config; misconfigured workspace provider fails tool construction with a naming error
- [x] 3.5 Runner tests: gate overrides allowlist and per-turn override; alias expansion; fs tool on/off matrix; legacy `browser.*` names still resolve

## 4. Browser configuration & enrichment

- [x] 4.1 SPIKE: CDP accessibility snapshot via go-rod (`DOMSnapshot.captureSnapshot`); decide ref format; write findings in the task notes before 4.2
  - FINDINGS: rod v0.116.2 exposes `Accessibility.getFullAXTree` (AX nodes carry `BackendDOMNodeID`), `DOM.ResolveNode` → `Page.ElementFromObject`, `Browser.Pages`. Ref format chosen: the backend DOM node ID string; each snapshot records its ref set and ref-targeted tools validate against it (no `DOMSnapshot.captureSnapshot` needed). Drag composed from rod mouse primitives (`MoveTo`/`Down`/`MoveLinear`/`Up`) — no `DragTo` helper in this version.
- [x] 4.2 `BrowserManager` reads workspace config (headless, remote_cdp_url, max_pages, idle_timeout_seconds, action_timeout_seconds); per-call rod context timeout; idle GC; headless ignored when CDP set
- [x] 4.3 `browser.snapshot` tool (ref-carrying snapshot)
- [x] 4.4 Ref-targeted tools: `browser.click`, `browser.type`, `browser.hover`, `browser.drag`, `browser.select_option` — each validates the ref against the last snapshot and returns a fresh snapshot
- [x] 4.5 Update existing `browser.act/read/screenshot` for shared timeouts + max_pages; availability error path unchanged
- [x] 4.6 Browser tool tests: ref targeting, timeout bounds, idle teardown, max pages cap, per-execution session isolation

## 5. Frontend — agent dialog

- [x] 5.1 `api.tools.list(wsId)` client; replace `TOOLS` const + dead `TOOL_ICON` map in `AgentConfigModal`
- [x] 5.2 Step 3 / Capabilities tab: flat iconed chips from catalog display names; Browser single chip (select = `browser` alias; hydrate from any `browser.*` name; normalize on save); Shell single chip (`execute`)
- [x] 5.3 Workspace-disabled state: greyed unselectable chip with lock + tooltip "Disabled in Settings → Tools"
- [x] 5.4 Icon set additions (`folder`, `file-plus`, `edit`, `scan`, `compass`); responsive check 360px→1920px for the chip row
- [x] 5.5 Component tests: catalog-driven render, alias normalize, disabled chip, legacy hydration

## 6. Frontend — settings Tools pane

- [x] 6.1 `SETTINGS_SECTIONS` gains `tools` (icon `zap`, after Skills); route handling unchanged
- [x] 6.2 `ToolsPane.tsx` (API-backed, ProvidersPane pattern): flat rows — display name, description, toggle; guard rejections as toasts
- [x] 6.3 Gear button + config dialog rendered from `ConfigField[]` (secret write-only with hint, text, number, boolean, enum select); inline validation errors; Save → PATCH; disabled-until-configured hint on the row
- [x] 6.4 Component tests: toggle drives PATCH, dialog renders per schema, secret never echoed, 422 fields surface inline

## 7. Verification

- [x] 7.1 `go build ./... && go vet ./...`; unit suites green; integration tests with `TEST_DATABASE_URL`
- [x] 7.2 `scripts/smoke.sh`: extend with tools GET/PATCH round-trip (superadmin), gate effect on agent create → run surface
- [ ] 7.3 Manual pass per UI contract: agent dialog chips (flat, iconed, Browser/Shell single), settings list + two dialogs, 360px + 1920px
- [x] 7.4 Cross-check both pending changes' interactions: `enrich-agent-tools` archive order documented; allowlist semantics tests updated for fs names + alias
