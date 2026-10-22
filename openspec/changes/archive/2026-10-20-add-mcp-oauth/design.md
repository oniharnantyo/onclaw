# Design

## Context

The MCP client (`internal/agents/mcp/connection.go`) dials stdio, streamable HTTP, and SSE transports with static configured headers only, then runs the initialize handshake bounded by `MCPManager.DefaultConnectTimeout` (10s, shared with `ProbeMCPServer`). mcp-go v0.43.0 — already pinned — ships the full OAuth 2.1 client stack (`client/transport/oauth.go`: metadata discovery, dynamic client registration, PKCE, code exchange, refresh, automatic Authorization-header injection, and an `OAuthAuthorizationRequiredError` that carries its handler out of the transport). The store persists `mcp_servers` rows with an `enabled` flag the runtime already honors (`internal/agents/mcp.go:66`); the pane buries the toggle in the edit dialog and renders status as tinted chips. Services stay Wails-free behind narrow seams (`Emitter` is the precedent); `internal/cli/app.go` is the composition root.

Verified against the live Atlassian server: an unauthenticated initialize returns `401 {"error":"invalid_token"}` with `WWW-Authenticate: Bearer resource_metadata="https://mcp.atlassian.com/.well-known/oauth-protected-resource/v2/mcp"`, and only the path-suffixed well-known URL is served — the origin-level one 404s.

See proposal.md for motivation and scope.

## Goals / Non-Goals

**Goals:**

- Generic MCP-spec OAuth 2.1 client authentication: nothing in the Go code may special-case Atlassian.
- Durable credentials for the gateway: authorize once on desktop, refresh forever headless.
- Row-level connection control: enable/disable, refresh-as-reconnect, glanceable state, and a clear action when user authorization is the missing piece.

**Non-Goals:**

- Per-server scope editing; client-credentials or device-code grants; OAuth for stdio.
- Encrypting tokens at rest beyond what provider API keys receive today.
- Changing the agent runtime's tool-assembly or degradation behavior (design D7 of port-agent-runtime stands).

## Decisions

**D1 — Build on mcp-go's OAuth stack; buddies supplies only the discovery pre-step.** Hand-rolling OAuth (PKCE, state, DCR, refresh) duplicates 700 lines of tested library code. The one gap is real, though: mcp-go's built-in discovery fetches `<origin>/.well-known/oauth-protected-resource`, which path-scoped deployments like Atlassian do not serve (verified). Buddies therefore parses the 401's `WWW-Authenticate` header (RFC 9728 §5: the header names the exact metadata document), fetches it itself, and passes `AuthServerMetadataURL` explicitly (`<issuer>/.well-known/oauth-authorization-server`). mcp-go's fallback discovery is never relied on — its `metadataOnce` would cache wrong endpoints.

**D2 — Authorization is out-of-band, not inside connect.** The browser round-trip is human-timescale; the connect path is bounded at 10s. The flow runs as a separate service action (`AuthorizeMCPServer`): fetch resource metadata (discovered by a bounded probe), register or reuse the client, start a loopback listener, open the system browser, and return immediately. The callback goroutine exchanges the code, persists tokens, emits `mcp:oauth:completed {serverID, ok}`, and re-probes the row. A 5-minute callback timeout shuts the listener down and re-marks `auth_required`. The bounded paths never block on a human.

**D3 — Token storage behind the package's narrow-data seam.** `Connection` gains OAuth settings plus a `TokenStore` field (mcp-go's two-method interface). The mcp package still accepts data, not the store (its design D1): the sqlite-backed implementation lives in the store layer, and a `func(serverID) TokenStore` factory is injected at composition into both the ref-hydration path (`agents.MCPServerRef`) and the service flow. A **per-server mutex inside the sqlite store** serializes GetToken/SaveToken: probes build separate handler instances from the manager's cached connection, mcp-go refreshes inside request threads, and if the authorization server rotates refresh tokens, a concurrent unsynchronized refresh would invalidate the winner's saved rotation.

**D4 — Persist the registration, not just the tokens.** `mcp_servers.auth` (JSON: `mode: none|oauth`, `client_id`, `issuer`, `resource_metadata_url`) and `mcp_oauth_tokens (server_id PK, token JSON, updated_at)`, migration `000012` (schema-version pin moves 11 → 12). The registered client id survives Disconnect so re-authorization never re-registers. Redirect URI: a fixed default `http://127.0.0.1:29173/callback` registered at DCR; if the port is taken at flow time, re-register against a fresh ephemeral port and update the stored registration. Alternative considered — ephemeral every time — rejected: it re-registers before every authorization and churns server-side client records.

**D5 — Status becomes a four-value vocabulary.** `unknown | connected | error | auth_required`, persisted by the probe. Classification rule: a dial/handshake 401 whose `WWW-Authenticate` header names a metadata document that decodes to a valid protected-resource response is `auth_required` — anything else that fails is `error`. The runtime treats `auth_required` like any failed connection (skip and mark); only the pane gives it a distinct action.

**D6 — Refresh = invalidate, then probe.** `ProbeMCPServer` invalidates the manager's cached entry before dialing, so a successful refresh swaps the runtime's credentials immediately instead of after the 30s error-retry or 10-minute idle-TTL windows. Always-invalidate (even on probe failure) is chosen over invalidate-on-success: it keeps "what the row shows" and "what agents use" from diverging, and a probe is rare and explicit. Refresh all fans out from the frontend across enabled servers with per-row loading state — no bulk service endpoint.

**D7 — Row toggle keeps backend semantics; add enable-probes.** Toggling off writes `enabled=false` through `UpdateMCPServer`, whose existing invalidation drops the live connection. Toggling on writes `enabled=true` and then probes, so the dot turns green without waiting for an agent turn. The runtime's opt-in gate (`!server.Enabled` in `agents/mcp.go`) is untouched.

**D8 — The browser is a host seam.** Services get `URLOpener interface { Open(url string) }` beside `Emitter`. The desktop host implements it with Wails' browser-open; the server host logs and the flow fails with a message directing the user to authorize from the desktop app. Tokens live in the shared `~/.buddies` database, so a gateway consumes and refreshes credentials authorized on the desktop with no further interaction — that is the intended topology, not a workaround.

**D9 — Scopes are discovered, not configured.** The flow requests the protected resource's advertised `scopes_supported` (including `offline_access`, which buys the refresh token). Matches what MCP-conformant clients do; no user-facing editor. Disconnect fetches the authorization-server metadata (raw JSON decode — mcp-go's metadata struct omits `revocation_endpoint`) and revokes at that endpoint when advertised, then deletes the stored tokens; the row returns to `auth_required`.

**D10 — Delta strategy for the pending capability.** `agent-mcp` exists only as the unarchived `port-agent-runtime` delta. This change's agent-mcp delta uses ADDED requirements with names disjoint from that delta's five (`Server transports`, `Probe and tool discovery`, `Namespaced tool naming`, `Opt-in policy`, `MCP tool execution`), so the two merge cleanly in either sync order. **`port-agent-runtime` must be synced or archived before this change is archived.** The settings delta is a MODIFIED of the existing `MCP pane` requirement (header and existing scenario names preserved verbatim — strict validation rejects renamed scenarios).

## Risks / Trade-offs

- [Refresh-token rotation racing between probe and runtime handlers] → per-server mutex in the sqlite token store (D3); a lost race degrades to re-authorization, never to a silently dead server.
- [mcp-go caches wrong endpoints in `metadataOnce`] → `AuthServerMetadataURL` is always set explicitly from the resource metadata buddies fetched (D1); fallback discovery is dead code on this path.
- [Fixed callback port squatted by another process] → ephemeral-port fallback with one DCR re-registration (D4); bounded and rare.
- [`auth_required` misclassification on non-OAuth 401s] → classification requires a well-formed protected-resource document behind the header's URL, not just the header's presence (D5).
- [Plaintext tokens in SQLite] → same exposure as provider API keys; revocation on Disconnect (D9) and short access-token lifetimes bound the blast radius.
- [Probe of an OAuth server without stored tokens now always reports `auth_required`] → intended; the negative cache and `Refresh all` keep the pane honest without spamming the authorization flow.

## Migration Plan

Migration `000012` is purely additive (`auth` column defaulting to `'{}'`, new `mcp_oauth_tokens` table); no backfill, rollback is drop-column/drop-table. Schema-version pin tests move 11 → 12. Frontend bindings regenerate after `task build` (the stale-dist gotcha). No behavior changes for servers that keep `auth.mode = none`.

## Open Questions

None blocking. The fixed callback port (29173) is revocable at task time if it collides with anything buddies ships later; nothing else is deferrable without changing specs or tasks.
