# Proposal

## Why

Remote MCP servers that follow the MCP authorization specification — Atlassian's `mcp.atlassian.com/v2/mcp` today, and the same pattern at Linear, Notion, GitHub, and Cloudflare — reject buddies' handshake with `401 {"error":"invalid_token"}` because the MCP client only sends the static headers configured on a server row and has no OAuth flow. Separately, the MCP pane gives no row-level control over a server's enabled state, no way to refresh live connections, and no glanceable connection state — and when a server demands authorization there is nothing to click. The buddies direction (a background gateway that keeps serving agents headlessly) makes durable, self-refreshing server credentials a prerequisite, not a convenience.

## What Changes

- **OAuth 2.1 client authentication for HTTP and SSE MCP servers.** On a 401 whose `WWW-Authenticate` header names an OAuth protected-resource metadata document, buddies discovers the authorization server (RFC 9728 / RFC 8414), performs dynamic client registration, and runs an authorization-code + PKCE flow through the user's system browser with a loopback redirect. Tokens persist in SQLite and refresh automatically on every subsequent connect — no browser needed once authorized.
- **Authentication choice per server.** HTTP/SSE server definitions gain an Authentication setting: *None* (today's header-based behavior, which remains first-class for API-token servers) or *OAuth 2.1*.
- **A fourth connection state, `auth_required`.** The probe distinguishes "needs user authorization" from a generic failure; the pane renders it with an **Authorize** action instead of an error string. When OAuth is not yet enabled but the server advertises it, the row offers to enable it.
- **Out-of-band authorization flow.** Because the interactive dance is human-timescale and the connect path is bounded at ten seconds, authorization runs as a separate service action: it opens the browser and returns immediately; completion is announced as an event that re-probes the row.
- **Refresh means reconnect.** The per-row Probe becomes **Refresh**, defined as *invalidate the cached runtime connection, then probe*, so agents pick up new credentials immediately rather than after the negative-cache or idle-TTL windows. A pane-level **Refresh all** runs it across every enabled server.
- **Row-level enabled toggle.** The toggle moves onto the server row (it remains in the edit dialog). Disabling drops the live connection immediately; enabling triggers a probe so the state becomes visible without waiting for an agent turn.
- **Status dots.** Each row leads with a colored dot — success-green connected, danger-red failed, warn-amber authorization required, neutral un-probed — alongside the existing status chip, in both themes.
- **Disconnect.** An OAuth-authenticated server offers Disconnect: revoke the token at the advertised revocation endpoint when there is one, delete the stored tokens, and return the row to `auth_required`. The dynamic-client registration is kept so re-authorizing never re-registers.
- **Storage.** Migration `000012`: an `auth` JSON column on `mcp_servers` (mode, registered client id, issuer, resource metadata URL) and an `mcp_oauth_tokens` table keyed by server id.
- **Server mode.** Headless hosts cannot run the browser flow; they consume and refresh tokens that were authorized on the desktop, which share the same `~/.buddies` database. A server that needs fresh authorization in server mode reports `auth_required` with a message directing the user to the desktop app.

Out of scope: a per-server scope editor (the flow requests the resource's advertised scopes), OAuth for stdio servers (not applicable), client-credentials or device-code grants, and encrypting tokens at rest beyond the protection provider API keys already receive.

## Capabilities

### New Capabilities

- `agent-mcp`: OAuth 2.1 server authentication, the `auth_required` connection state, and refresh-as-reconnect semantics. **Note:** the `agent-mcp` capability is declared by the unarchived `port-agent-runtime` change and does not yet exist under `openspec/specs/`. This change's delta therefore ADDS requirements under names disjoint from that pending delta (`Server transports`, `Probe and tool discovery`, `Namespaced tool naming`, `Opt-in policy`, `MCP tool execution`) so the two merge cleanly in either sync order. `port-agent-runtime` should be synced or archived before this change is archived.

### Modified Capabilities

- `settings`: the **MCP pane** requirement gains the Authentication setting on HTTP/SSE servers, the row-level enabled toggle, the Refresh and Refresh-all actions, the status dot vocabulary, and the Authorize / Disconnect actions for the `auth_required` and OAuth-connected states.

## Impact

- **Go — MCP client** (`internal/agents/mcp/`): `Connection` grows OAuth configuration plus a narrow `TokenStore` seam (the package keeps accepting data, not the store); `dial` switches HTTP/SSE to mcp-go's OAuth-aware constructors when the server's auth mode is OAuth; `connect`/`Probe` classify the OAuth-authorization-required error distinctly. `MCPManager` is unchanged. Uses mcp-go v0.43.0's existing `client/transport` OAuth stack (already pinned) — **no new dependencies**.
- **Go — ref hydration** (`internal/agents/mcp.go`): `MCPServerRef` loads the server's auth settings and a per-server token store into the `Ref`; the runtime path is otherwise untouched — expired tokens refresh inside the transport, and a refresh failure degrades through the existing design-D7 skip-and-mark behavior.
- **Go — services** (`internal/services/`): new authorization-flow service method and completion event; `ProbeMCPServer` gains invalidate-first semantics; a new host seam for opening a URL in the system browser (sibling of `Emitter`, keeping services Wails-free), implemented by the desktop host and a logging no-op on the server host.
- **Go — store** (`internal/store/`, `internal/store/sqlite/`): `MCPServer` model gains the auth settings; new token persistence with a per-server mutex around saves (refresh-token rotation race); migration `000012` (schema-version pin tests move to 12).
- **Go — composition** (`internal/cli/app.go`): wires the token-store factory and URL opener alongside the existing `WithMCPInvalidate`.
- **Frontend** (`frontend/src/features/settings/components/McpPane.tsx`, settings `types.ts`, `lib/client.ts`, regenerated Wails bindings): Authentication select in the dialog, row toggle, status dots, Refresh / Refresh all / Authorize / Disconnect actions, and the completion-event listener. Both themes.
- **Tests**: mcp package tests against an httptest OAuth authorization server and protected resource (mcp-go's own OAuth tests are the reference), sqlite token-store tests, service flow tests with a fake URL opener and emitter, and McpPane tests for the new row states and actions.
