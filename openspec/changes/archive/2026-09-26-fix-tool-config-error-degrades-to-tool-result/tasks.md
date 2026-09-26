# Tasks

## 1. Lazy web.search provider resolution

- [x] 1.1 In `internal/agents/tools/websearch.go`: add a lazy-provider option — a resolver closure `func() (SearchProvider, error)` on `webSearchTool`, invoked on first `InvokableRun` and memoized per instance (`sync.Once`, caching the error too, design D2). `NewWebSearch` keeps its schema and only errors when neither an eager provider (test seam) nor a resolver is set. Update the package doc's "fails construction" sentence. Verify: `go build ./...`.
- [x] 1.2 In `internal/agents/tool_registry.go`: the `web.search` registration becomes infallible — pass `func() (tools.SearchProvider, error) { return searchProviderFor(tctx) }` as the resolver; keep `searchProviderFor` and its error texts unchanged; update the `errWebSearchNotConfigured` comment (construction-error language → invocation-time degradation, design D1/D4). Verify: `go build ./...`.
- [x] 1.3 Flip `internal/agents/tool_registry_test.go` (line ~324): an unconfigured workspace now resolves `web.search` successfully, and invoking it returns the not-configured error (via the middleware contract, an `errorResultPayload`-shaped result naming `web.search`); a configured workspace still resolves through its chain. Verify: `go test ./internal/agents/...`.
- [x] 1.4 Add a `websearch_test.go` case: resolver error surfaces from `InvokableRun` on first call and identically on a second call (memoized, resolver called once); resolver success resolves the chain and serves a query. Verify: `go test ./internal/agents/tools/...`.

## 2. File-family construction self-heal

- [x] 2.1 In `internal/agents/tools/delete_file.go`, `document_read.go`, `document_create.go`: replace the `stat`-and-fail agent-dir checks with `os.MkdirAll` (design D5) — an empty path still fails, a missing directory is created, a non-directory or unwritable path still errors with the existing wording. Verify: `go build ./...`.
- [x] 2.2 Add ctor tests for each of the three tools: missing dir is created and the tool builds; a file occupying the path still fails construction. Verify: `go test ./internal/agents/tools/...`.

## 3. Verification

- [x] 3.1 Full backend suite green: `go vet ./... && go test ./...`. Verify: clean output.
- [x] 3.2 Smoke suite: `./scripts/smoke.sh` (fresh `onclaw_smoke` DB, full log) — all sections pass; no section depends on the old construction-failure behavior. Verify: full log green.
- [ ] 3.3 Live pass: in a workspace with no search provider configured, send a prompt that makes the agent call `web.search` — the run completes, the tool-call card shows the not-configured error, and the agent relays the fix / answers another way. Verify: transcript observation.
