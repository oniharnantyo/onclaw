## 1. Provider registry: remove DuckDuckGo

- [x] 1.1 Delete `duckduckgoProvider`, its HTML extraction helpers, the `SearchCredentialNone` kind, `WithUserAgent`, and the legacy `NewSearchProvider` constructor from `internal/agents/tools`; drop the DDG registry entry and `EnvSearchProvider`-default-to-DDG constant usage
- [x] 1.2 Remove or rewrite the DDG scrape tests in `websearch_test.go`; keep tavily-provider tests green
- [x] 1.3 Check whether `golang.org/x/net/html` is still imported anywhere (`webfetch`); if not, drop the dependency and run `go build ./... && go vet ./...`

## 2. Chain provider and timeout

- [x] 2.1 Add `chainProvider` implementing `SearchProvider` over an ordered `[]SearchProvider` (unexported): any attempt error advances, 200-with-zero-results returns as a valid answer without advancing, all failing returns the last error prefixed with the entry name ("web.search: Exa 1 returned status 429")
- [x] 2.2 Wrap each entry's provider construction with a per-attempt `http.Client` timeout built from `request_timeout_seconds`; table-test the window: success-first, failover-on-429/5xx/network/decode, all-fail last-error naming, empty-success no-advance, timeout advance

## 3. Settings service: entries config

- [x] 3.1 Update `webSearchConfigSchema` in `tool_catalog.go`: `entries` (provider-stack list: name text, provider enum, api_key secret, base_url text with per-entry show_if provider=searxng) + flat `request_timeout_seconds` number (default 10, max 60); remove flat `provider`/`api_key`/`base_url` fields
- [x] 3.2 Make secret machinery entry-aware in `toolsettings.go`: encrypt/decrypt/hint per entry credential, hints nested in the entry and stripped from API reads; server assigns stable entry `id` on first persist; upsert semantics: known id + empty credential keeps stored, new entry missing credential → 422
- [x] 3.3 Entries validation: unique non-empty name (case-insensitive), known provider, credential per kind, ≥1 fully valid entry to enable, 20-entry cap; failures return `ConfigValidationError` naming the offending entry; replace `validateCredentialRequirements`/`configSatisfiesRequirements` flat logic
- [x] 3.4 Rewrite `mergeEnvFallbacks` for web.search: empty entries only, env provider+key seed a single "Instance default (env)" entry; env provider missing its key or `duckduckgo` → treated as unset/construction error
- [x] 3.5 Unit-test the settings service: merge/keep-secret-by-id across reorder, duplicate-name 422, enable gating, env fallback seeding, cap 20

## 4. Runtime resolution

- [x] 4.1 Rewrite `searchProviderFor` in `tool_registry.go`: decrypt entries via settings service, build per-entry providers, chain the first three; empty entries + no env → construction error "web.search is not configured — add a provider in Settings → Tools"
- [x] 4.2 Update `tool_registry_test.go` execution-path tests for the entries config shape and unconfigured error path

## 5. Migration 000023

- [x] 5.1 Write `migrations/000023_web_search_provider_stacks.{up,down}.sql`: up re-nests flat `web.search` rows into `{entries:[{id,name:"<Label> 1",...}]}` (string moves only, generated id), `provider=duckduckgo`/missing-provider rows → `{entries:[]}`; down best-effort restores entry 0 to flat
- [x] 5.2 Integration test (`-tags=integration`): migrate a fixture flat row (tavily + envelope) up, assert entry shape and unchanged envelope ciphertext, assert ddg row → empty entries, down restores

## 6. API surface

- [x] 6.1 Update tools handlers/config view (`internal/server/handlers`) so `web.search` config reads return entries with hints and writes accept the entries payload; update handler tests including the 422 paths (duplicate name, missing credential, cap, enable gating)

## 7. Web: stack editor dialog

- [x] 7.1 Extend `web/src/lib/api.ts` + `toolCatalog.ts` types for entries config (entry: id/name/provider/api_key hint/base_url) and the timeout field
- [x] 7.2 Rebuild the `web.search` dialog in `ToolsPane.tsx` as the provider-stack list editor per design.md mockups: per-row [↑][↓] (bounds-disabled), name input, provider select, credential field (write-only, last-4 hint placeholder, base-URL field when provider=searxng), remove with undo toast (no persist until Save), Add control, rows 1–3 labeled "in rotation" / rest dimmed "standby" with live re-evaluation on reorder, flat timeout field
- [x] 7.3 Save path: submit whole ordered list with entry ids; empty credential + known id keeps stored; per-row inline validation (empty name, missing credential, duplicate names) blocks Save; update ToolsPane tests (select mocks must include every option a test selects)
- [x] 7.4 Empty state copy: "No providers configured — web.search will error until you add one."; Tools row summary line shows "N configured · 3 stacked" when entries exist

## 8. Verification & docs

- [x] 8.1 `go build ./... && go vet ./... && go test ./...`; run web build and touched vitest suites (pre-existing localStorage jsdom failures are out of scope)
- [x] 8.2 Update AGENTS.md: replace the "web.search defaults to DuckDuckGo scraping / zero external credentials" wording with provider-stacks + fail-fast-unconfigured behavior
- [ ] 8.3 Manual browser pass (5.3-style): configure Tavily 1/Tavily 2/Exa 1, reorder, confirm in-rotation/standby labels, save/reload shows hints, disable+enable gating
