## Context

The web console (`onclaw serve`) already manages providers, agents, skills, and hooks through a
consistent layer: a `service.Service` holding the relevant `store` interfaces, thin HTTP handlers
in `internal/api/handler/`, routes registered in `internal/api/routes.go` behind `requireAuth`, and
a React page per concern in `web/src/components/`. MCP was shipped **CLI-only** by the archived
`add-mcp-tools` change: the `MCPServer` DTO, `MCPServerStore`, the `internal/mcp` package
(`Manager` + `NewClient`), agent wiring, and `onclaw mcp add/list/remove/test` all exist, but there
is no `/api/mcp` surface and no console page.

A second problem: `serve` builds one `mcp.Manager` for the process lifetime. Its `Tools()` caches
the discovered tool set after the first call (`internal/mcp/manager.go`), so edits made while the
server runs never reach the live chat agent without a restart. The CLI avoids this only because it
re-assembles the agent per command; the long-lived API server does not.

## Goals / Non-Goals

**Goals**
- Full MCP server CRUD (+ enable/disable toggle + connection test) over an authenticated REST API,
  mirroring the hooks feature end to end.
- A console page that lets a user add, edit, test, toggle, and remove MCP servers without leaving
  the browser.
- Edits made via the API take effect on the running agent on the next chat turn (live reload).

**Non-Goals**
- Env-var encryption: stored plaintext, redacted in API/UI (carried over from `add-mcp-tools`;
  `${secret:…}` → `SecretStore` is a later change).
- Tool-name namespacing across servers.
- CLI changes (the existing `onclaw mcp` commands are untouched).
- Reconnection of an in-flight turn mid-generation; reload applies to the next assembled agent.

## Decisions

- **Mirror the hooks feature exactly.** Service methods on `Service` (new `mcp.go`), handlers in a
  new `handler/mcp.go`, routes beside the `/api/hooks` block, DTOs in `service/types.go`. This is
  the lowest-risk path: the pattern is proven, reviewers know it, and it keeps the diff surgical.
  *Alternative:* a versioned `/api/v1/mcp` namespace — rejected; no other console resource is
  versioned.

- **Live reload via a `Manager.Reload()` + an injected callback, not a direct import.** Add
  `Reload()` to the `Manager` interface (it reuses the reset already in `Close()`: lock, close +
  drop clients, clear the cache so the next `Tools()` re-reads the store). `Service` receives a
  `reloadMCP func()` field — the same callback-injection style already used for `resolve` — so
  `internal/api/service` imports no MCP package. Mutating service methods call it (nil-guarded).
  *Alternative:* inject the `*mcp.Manager` directly into `Service` — rejected to preserve the
  existing layering (the service package currently depends only on `store`, `llm`, `skill`, `hooks`).
  *Alternative:* reuse the CLI's SIGHUP path — rejected; the `serve` SIGHUP handler reloads only the
  LLM profiles today, and round-tripping through a signal for an in-process mutation is needlessly
  indirect.

- **Env values are redacted in read responses; stored plaintext.** List/get return an
  `MCPServerView` whose env values are replaced with `***` (keys preserved), matching CLI `mcp list`
  redaction and the never-return-secrets rule. Mutating inputs decode directly into
  `store.MCPServer` (as hooks decodes into `store.Hook`).

- **Toggle via `UpdateServer`, not a new store method.** `MCPServerStore` has no `Toggle`; toggle is
  implemented in the service as Get → flip `Enabled` → `UpdateServer`. Avoids widening the store
  contract for a single-bit change.

- **`POST /api/mcp/test` takes a config body (test-before-save).** Mirrors `POST /api/hooks/test`:
  the modal can validate an unsaved server by connecting via `mcp.NewClient` and listing tools,
  identical to `onclaw mcp test`.

- **Transport-aware UI form.** The Add/Edit modal renders stdio fields (command/args/env) or
  http/sse fields (url) based on the selected transport, and validates client-side before submit.

## Risks / Trade-offs

- **Reload racing an in-flight `Tools()`** → `Reload()` takes the same `mu` as `Tools()`/`Close()`,
  so a reload either completes before or waits out an in-progress discovery; no torn state.
- **Plaintext env secrets in SQLite** → redacted everywhere they're surfaced; encryption deferred
  (see Non-Goals). Documented so it is not mistaken for secure storage.
- **stdio subprocess memory on SBC** → unchanged from `add-mcp-tools`; the UI recommends remote
  (http) servers where practical. Out of scope to fix here.
- **Manager cache staleness window** → between a mutation and the next `Tools()` call the old tool
  set may be served for an already-running turn; acceptable, since reload targets the next turn.
