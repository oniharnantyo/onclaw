## Why

`web.search` currently binds a workspace to exactly one search provider with one credential, and its zero-credential default (DuckDuckGo HTML scraping) is unreliable — routinely blocked by ISPs or by DuckDuckGo itself, producing opaque failures mid-chat. Workspaces that exhaust a provider's rate limits (or whose free default is blocked) have no way to spread load across multiple keys or fall back to a second provider.

## What Changes

- **BREAKING** — `web.search` workspace config changes shape from a flat `{provider, api_key, base_url}` record to an ordered list of named provider entries (`entries: [{id, name, provider, api_key?, base_url?}]`), with a flat `request_timeout_seconds` field (default 10) applied per attempt.
- A workspace may configure any number of entries; the same provider may appear multiple times with different keys (e.g. "Tavily 1", "Tavily 2", "Exa 1"). List order is priority.
- At request time the resolver builds a failover chain from the first **three** entries (positional, platform constant) and tries them in order: any attempt error (non-200, network, decode) advances to the next entry; a 200 with zero results is a valid answer; all three failing returns the last error, named after the entry ("web.search: Exa 1 returned status 429"). Entries below the window never serve until reordered in.
- **BREAKING** — the DuckDuckGo scraping provider is removed entirely (provider registry, client, HTML parser, `SearchCredentialNone` credential kind). With no entries and no instance env provider, `web.search` fails construction with an explicit "not configured" error instead of silently scraping.
- Instance env fallback survives but never silently defaults: `ONCLAW_SEARCH_PROVIDER` + `ONCLAW_TAVILY_API_KEY` seed a fallback entry only when the list is empty; env naming a provider whose credential is absent is a construction error.
- Migration 000023 converts existing flat settings rows to the entries shape; rows selecting DuckDuckGo become explicitly unconfigured (empty entries). Secret envelopes are re-nested without re-encryption (AAD is workspace-scoped).
- The Tools pane config dialog for `web.search` becomes a provider-stack list editor: rows with reorder controls, name, provider select, per-row credential field (API key with last-4 hint, or base URL for SearXNG), remove, and Add; rows inside the window are labeled "in rotation", the rest dimmed "standby".
- Enabling `web.search` requires at least one fully valid entry (unique non-empty name, known provider, credential present per kind); duplicate names are rejected.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `workspace-tools`: the "Search provider configuration" requirement is rewritten — ordered named provider entries replace the single provider record; a 3-deep positional failover window with any-error advance and per-attempt timeout governs request resolution; DuckDuckGo is removed and an unconfigured tool fails fast instead of falling back to scraping.
- `web-app/settings`: the "Tools pane (API-backed)" requirement extends the config dialog contract for `web.search` — a provider-stack list editor (reorderable named rows, per-row credential, in-rotation/standby states) replaces the flat field form.

## Impact

- **Backend:** `internal/agents/tools` (websearch.go, websearch_registry.go — remove DDG, add chain provider), `internal/agents/tool_catalog.go` (web.search config schema), `internal/agents/toolsettings.go` (entries validation, per-entry secret encrypt/decrypt/hints, env fallback, credential gating), `internal/agents/tool_registry.go` (`searchProviderFor` → chain resolution), `migrations/000023_*` (flat → entries JSONB rewrite), `internal/server/handlers` (config view/upsert payloads).
- **Frontend:** `web/src/screens/settings/ToolsPane.tsx` (stack list editor dialog), `web/src/lib/api.ts` (config field/entry types), `web/src/lib/toolCatalog.ts`.
- **Docs/tests:** AGENTS.md zero-credential wording, DDG scrape tests removed/replaced by chain failover tests, smoke.sh search coverage if present.
- **API:** workspace tool settings config payload shape changes (breaking for any API consumer writing `web.search` config directly).
