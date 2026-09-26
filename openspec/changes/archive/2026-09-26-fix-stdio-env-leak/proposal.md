## Why

Every stdio MCP server OnClaw launches currently receives the server process's **entire parent environment** — mcp-go merges the configured env rows over the parent environment — so any MCP child (a recipe-bundled server, a community npm package, a compromised binary) can read `DATABASE_URL`, the JWT secret, and hook secret material directly from its own process env. A peer implementation (Hermes) deliberately passes only explicit env plus a safe baseline for exactly this reason. The fix is small, isolated, and closes a real exfiltration path in a self-hosted multi-tenant product.

## What Changes

- stdio MCP children now start with a **constructed environment**: a safe baseline (PATH, HOME, TMPDIR, LANG/LC_*, TZ, and the system proxy variables) plus exactly the configured `Env` rows — never the parent process's remaining variables.
- The baseline is a fixed allowlist documented in the capability spec; configured rows still override baseline entries (a user-set `PATH` or `HTTPS_PROXY` wins).
- stdio probe and run dials share the same behavior — no split brain between "test" and "run" environments.
- HTTP/SSE transports are untouched (they never inherited the parent environment).
- **BREAKING** (behavioral, deliberate): a stdio MCP server that silently depended on an inherited variable (e.g. reading `ONCLAW_DATABASE_URL` from the parent) now sees an empty value. This is the point of the change; the release note must call it out.

## Capabilities

### New Capabilities

- (none)

### Modified Capabilities

- `workspace-mcp`: new security requirement governing the stdio child environment — constructed baseline + configured rows only, parent variables excluded; probe and run dials behave identically.

## Impact

- `internal/agents/mcp/client.go` (`dial`, stdio branch): replace `envSlice(conn.Env)` passthrough with baseline+rows construction.
- `internal/agents/mcp/client_test.go`, `manager_test.go`: env expectations.
- `internal/agents/mcp/testdata/mockmcpserver/main.go`: extend to echo received env for assertions.
- No API, schema, or web changes. Existing MCP server rows keep working; only inherited-variable dependencies break (intentionally).
