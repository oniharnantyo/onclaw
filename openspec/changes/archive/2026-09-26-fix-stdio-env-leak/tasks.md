## 1. Verify mcp-go env contract

- [x] 1.1 Read the pinned mcp-go stdio transport source: determine whether a supplied env slice replaces or merges with the parent environment (`cmd.Env` handling)
- [x] 1.2 Write the regression test first: extend `testdata/mockmcpserver` to echo selected received env vars; assert a dial with constructed env exposes baseline+rows and hides a planted parent-only var (`ONCLAW_ENVLEAK_CANARY`) (red, C1b greens it)

## 2. Constructed environment

- [x] 2.1 Implement the baseline allowlist constant (PATH, HOME, TMPDIR, LANG, LC_ALL, TZ, proxy vars upper+lowercase) in `internal/agents/mcp`
- [x] 2.2 Replace `envSlice` passthrough in the stdio dial branch with constructed env = baseline (if present on parent) ∪ configured rows, rows last so they win
- [x] 2.3 If mcp-go merges over parent regardless (per 1.1), pre-filter: pass constructed env by explicitly setting `cmd.Env` through the transport's exposed hook or upstream the one-line fix; document the chosen mechanism here (mechanism: `transport.WithCommandFunc` via `client.NewStdioMCPClientWithOptions`; the CommandFunc builds the `exec.Cmd` and sets `cmd.Env` to the constructed slice wholesale)

## 3. Coverage

- [x] 3.1 Unit tests: configured row overrides baseline (`PATH`), parent-only var absent, missing baseline vars omitted (not empty-string), rows with empty values preserved
- [x] 3.2 Probe-vs-run parity test: `Probe` and a manager-mediated dial construct identical env for the same connection
- [x] 3.3 URL-transport regression: header-only behavior untouched (existing tests green)

## 4. Verification

- [x] 4.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 4.2 Manual: run one real stdio server (e.g. the mock) via a workspace MCP server, confirm probe connects and tool calls work (automated stand-in: `TestProbeAndManagerDialConstructIdenticalEnv` dials the compiled mock server through `Probe` and a real-connector `MCPManager`, asserts a tool call works and the child env equals `constructChildEnv` output)
- [x] 4.3 Update AGENTS.md/domain comment where the old "merges over the parent environment" behavior is documented (client.go doc comment)
