## 1. Shared secret resolution

- [x] 1.1 Define a `SecretResolver` interface (`Resolve(ctx, envVar, secretKey) (string, error)`) in `internal/secrets/` — env-var name and SecretStore key are passed explicitly so one contract serves both the LLM layer (`ONCLAW_PROVIDER_*` / profile secrets) and web (`ONCLAW_WEB_*` / `web.*`)
- [x] 1.2 Refactor `llm.Service.ResolveSecret` (`service.go:210-228`) behind `SecretResolver` (behavior-preserving; it passes its own `ONCLAW_PROVIDER_<NAME>` env var + profile secret key)
- [x] 1.3 Add a `SecretResolver` field to `tools.Scope` and thread it through the assembly path: `internal/cli/agent_session.go` (`resolveAndAssemble`, `ToolGroupCfgWrapper` wired at :197) → `internal/agent/agent.go` (Agent field at :68, `tools.Scope` built at :112)

## 2. Web capability package (`internal/web/`)

- [x] 2.1 `internal/web/config.go` — `Config` DTO (`SearchProvider`, `FetchProvider`, `UserAgent`, `TimeoutSeconds`, `MaxBytes`, `GoogleCX`, `LightpandaBinPath`) + parse helper with defaults
- [x] 2.2 `internal/web/searcher.go` — `Searcher` interface, `SearchResult`, `searchers` map, `RegisterSearcher`, `LookupSearcher`
- [x] 2.3 `internal/web/fetcher.go` — `Fetcher` interface, `FetchResult`, `fetchers` map, `RegisterFetcher`, `LookupFetcher`
- [x] 2.4 `internal/web/ssrf.go` + `_test.go` — `ValidateURLNotInternal`, `isPrivateIP`, private CIDRs, metadata-host block, redirect re-validation

## 3. `edit_file` tool

- [x] 3.1 `internal/agent/tools/editfile.go` — `editFileTool` (Category `Filesystem`), exact-string replace requiring a unique match, `ValidatePath` confinement
- [x] 3.2 `editfile_test.go` — unique edit succeeds; not-found rejected; non-unique rejected; traversal blocked

## 4. Default web providers

- [x] 4.1 `internal/web/ddg/search.go` + `_test.go` — `RegisterSearcher("duckduckgo", …)`, DuckDuckGo HTML fetch + parse (`parseDDGResults`, `cleanDDGURL`, tag strip); fixture-based parse test (no network)
- [x] 4.2 `internal/web/http/fetch.go` + `_test.go` — `RegisterFetcher("http", …)`, stdlib GET + SSRF + redirect re-validation + `LimitReader` + HTML→text strip + truncate; `httptest.Server` fixtures

## 5. API search providers (stdlib `net/http` + `encoding/json`)

- [x] 5.1 `internal/web/tavily/search.go` + `_test.go` — `POST api.tavily.com/search`; key via `SecretResolver` (`ONCLAW_WEB_TAVILY_API_KEY` > SecretStore `web.tavily`); `httptest` fixture; missing-key error
- [x] 5.2 `internal/web/exa/search.go` + `_test.go` — `POST api.exa.ai/search` (`x-api-key`); `ONCLAW_WEB_EXA_API_KEY` > `web.exa`
- [x] 5.3 `internal/web/google/search.go` + `_test.go` — `GET customsearch/v1`; `ONCLAW_WEB_GOOGLE_API_KEY` > `web.google`; `google_cx` from Config

## 6. Lightpanda fetch provider

- [x] 6.1 `internal/web/lightpanda/fetch.go` + `_test.go` — `RegisterFetcher("lightpanda", …)`; `exec.CommandContext(binPath, "fetch", "--dump", "markdown", url)` (argv, no shell); SSRF pre-check; timeout; error on missing binary / non-zero exit; unit-test argv construction + SSRF + missing-binary path

## 7. Web tool layer (`internal/agent/tools/web/`)

- [x] 7.1 `register.go` — `web_search` + `web_fetch` `Tool`s (Category `Web`), `tools.Register` both, `tools.RegisterConfig("Web", schema, load, save)`, side-effect imports of all provider impl packages
- [x] 7.2 `websearch.go` — per call read `scope.ToolGroupCfg.GetConfig(ctx, "Web")`, resolve `SearcherFactory`, invoke; on absence/error/missing-key fall back to `duckduckgo` and prepend a notice
- [x] 7.3 `webfetch.go` — same for `FetcherFactory`, fallback to `http` with notice

## 8. Wiring & integration

- [x] 8.1 Verify new tools auto-seed into `tool_registry` (default enabled) — confirm `internal/store/sqlite/db.go` `SeedTools` iterates `tools.GetRegistry()` (loop at :256-258); `tool_registry.go` holds the table CRUD
- [x] 8.2 Verify `SecretResolver` is threaded into `Scope` at the tool-assembly site and web providers can resolve keys through it

## 9. Web category structured UI form

- [x] 9.1 `web/src/components/Tools.tsx` — add a Web-category branch (alongside the existing Browser custom-form branch) rendering **one field per config property**: `search_provider` / `fetch_provider` selects (populated from registered providers), request-timeout + max-response-bytes number inputs, user-agent text, `google_cx` text, `lightpanda_bin_path` text — never a raw-JSON `<textarea>` (per `CLAUDE.md` structured-fields rule; the current non-Browser fallback at `Tools.tsx:453` is exactly the anti-pattern to avoid)
- [x] 9.2 Validate the form against the registered Web JSON schema on save (the API already serves any category's schema generically via `tools.GetConfigEntry`, `internal/api/service/tools.go`); confirm no secret fields are ever present (keys resolve via `SecretResolver`, not config)

## 10. End-to-end verification

- [x] 10.1 `make fmt && make vet && make build` — confirm `CGO_ENABLED=0` and no `go.mod` dependency additions
- [x] 10.2 `go test ./internal/agent/tools/... ./internal/web/...` — all unit + fallback + SSRF + provider-swap tests pass
- [x] 10.3 Manual: `onclaw run` → agent calls `web_search`, `web_fetch` on a result, then `edit_file`; confirm the Web UI renders the **structured** Web-category form (no JSON textarea) and lists `edit_file` under `Filesystem`