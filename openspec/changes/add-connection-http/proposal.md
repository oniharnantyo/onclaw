## Why

Some services have no cloud-reachable MCP server — Figma's official server runs on the desktop app, unreachable from a deployed OnClaw — while their REST APIs are PAT-compatible. The agent has no credential-safe way to call those APIs: web fetch is deliberately credential-dark, and handing the model a token leaks it into context. This change adds connection-scoped HTTP: a service connection whose agent-facing surface is a single request tool, pinned to the recipe's base URL, with credentials injected server-side.

## What Changes

- **Connection kind `http`**: recipes may declare the http kind with a base URL, an auth header name, and a probe call. HTTP-kind connections do not materialize MCP servers; disconnect cascade, secrets handling, access levels, and status/probe semantics carry over unchanged.
- **One tool per recipe-declared verb**: the recipe declares the connection's curated tool surface as data — each verb names an HTTP method, a path template, typed parameters, and a description (e.g., `figma.get_comments`, `figma.list_files`). Verbs become real tools at resolution time through the connection tool source. **No free-form request tool exists**: agents can call only declared verbs, so the exposed surface is fail-closed and extending it is a recipe release, not a model improvisation.
- **Server-side credential injection**: the connection's secret header is attached at call time; the tool schema and description contain no credential material, so tokens never enter model context.
- **A connection tool source beside MCP**: the runner's tool assembly gains a seam that yields the declared verb tools of HTTP-kind connections, consulted in the same resolution pass as MCP tools (after built-ins, policy-governed).
- **Response discipline**: JSON/text bodies size-capped and truncated like web fetch, so a large API response cannot blow the turn budget.
- **Figma as the reference recipe** (read-mostly REST, PAT auth), live-verified at apply time.

## Capabilities

### New Capabilities

- `connection-http`: HTTP-kind service connections — pinned-base-URL request tools with server-side credential injection, the connection tool source seam, and probe/status semantics.

### Modified Capabilities

- `workspace-connections`: the materialization requirement narrows to MCP-kind connections — HTTP-kind connections contribute request tools instead of materializing an MCP server; all other connection semantics (secrets, access level, disconnect cascade, authorization) are unchanged and apply to both kinds.

## Impact

- **Domain** (`internal/domain`): connection kind values (`mcp` default, `http`), recipe fields (base URL, auth header name, probe call).
- **Runner** (`internal/agents`): new connection tool source consulted at tool resolution; a built-in-style request tool parameterized per connection.
- **Connections service**: kind-aware create/probe/disconnect (HTTP kind skips materialization; probe executes the recipe's call).
- **Web**: no new surfaces beyond kind-aware copy on cards and connect dialog (guided steps differ: API token + scopes instead of MCP wiring).
- **Smoke tests**: HTTP-kind connection lifecycle with a stub API.
