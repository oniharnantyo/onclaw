# Tasks

Integration contracts (names every task codes against; see design D1–D10):

- `store.MCPServer.Auth store.MCPServerAuth` — JSON column `mcp_servers.auth` (`mode`, `client_id`, `client_secret`, `issuer`, `resource_metadata_url`, `auth_server_metadata_url`, `redirect_uri`, `scopes`); `store.MCPAuthModeNone`/`MCPAuthModeOAuth`; `(MCPServerAuth).IsOAuth()`.
- `store.MCPOAuthToken{ServerID, Token []byte, UpdatedAt}` + `store.MCPOAuthTokenStore{Get, Save, Delete}` reached through `Store.MCPOAuthTokens()`.
- `mcp.StatusAuthRequired = "auth_required"`; `mcp.Connection.OAuth mcp.OAuth{Enabled, ClientID, ClientSecret, RedirectURI, Scopes, AuthServerMetadataURL, Tokens mcp.TokenStore}`; `mcp.ErrAuthRequired` (+ `*mcp.AuthRequiredError{Discovery, Cause}`); `mcp.Discover`, `mcp.FetchAuthServerMetadata`, `mcp.RevokeToken`, `mcp.StartAuthorization` (loopback flow, `DefaultRedirectURI = http://127.0.0.1:29173/callback`, 5-minute callback bound).
- `agents.MCPServerRef(server, tokens store.MCPOAuthTokenStore)`; `agents.NewMCPTokenStore(serverID, tokens) mcp.TokenStore` (the mcp-go adapter lives beside the ref hydration — no composition-root factory needed).
- `services.URLOpener{Open(url) error}` + `services.NoBrowser` (returns `ErrNoBrowser`); `services.EventMCPOAuthCompleted = "mcp:oauth:completed"` with `MCPOAuthCompletedEvent{ServerID, OK, Error}`; `SettingsService` gains `WithEmitter`/`WithURLOpener`; `cli.Assemble(ctx, emitter, opener)`.

## 1. Schema and store

- [x] 1.1 Migration `000012_mcp_oauth.sql`: `ALTER TABLE mcp_servers ADD COLUMN auth TEXT NOT NULL DEFAULT '{}'`; `CREATE TABLE mcp_oauth_tokens (server_id TEXT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE, token TEXT NOT NULL, updated_at TEXT NOT NULL)`; schema-version pins in `db_test.go` move 11 → 12. Verify: `go test ./internal/store/sqlite/...` including the migration-count test.
- [x] 1.2 `store.MCPServer` gains `Auth MCPServerAuth` (design D4 JSON shape, snake_case tags, `omitempty`); sqlite `mcpservers.go` reads/writes the `auth` column (`'{}'` decodes to the zero value = mode none); fake store clones `Auth.Scopes`. Verify: sqlite round-trip test for an OAuth-mode row and a legacy `'{}'` row.
- [x] 1.3 `store.MCPOAuthTokenStore` (interface file `mcpoauthtokens.go`), sqlite implementation with a **per-server mutex** serializing Get/Save/Delete (design D3), `Store.MCPOAuthTokens()` on the interface, sqlite, and fake. Verify: sqlite tests — absent → `ErrNotFound`, upsert overwrites, delete is idempotent, cascade on server delete, concurrent saves race-free under `-race`.

## 2. MCP client: OAuth transport and discovery

- [x] 2.1 `mcp.Connection.OAuth` + `StatusAuthRequired`; `dial` uses `client.NewOAuthStreamableHttpClient` / `client.NewOAuthSSEClient` with `transport.OAuthConfig{PKCEEnabled: true, AuthServerMetadataURL, TokenStore, ...}` when `OAuth.Enabled` (configured headers still applied); `Enabled` with nil `Tokens` fails fast. Verify: `go test ./internal/agents/mcp/...` — OAuth dial against an httptest MCP server that requires a Bearer token and an httptest token store.
- [x] 2.2 `mcp.Discover(ctx, transport, url, headers)` (design D1/D5): one unauthenticated request (POST initialize for http, GET for sse), parse `WWW-Authenticate … resource_metadata="…"`, fetch the protected-resource document (`authorization_servers`, `scopes_supported`), fetch + validate authorization-server metadata (RFC 8414 path-aware URL, then OIDC `/.well-known/openid-configuration`), raw-decode `registration_endpoint`/`revocation_endpoint`; `ErrNoOAuth` when the server does not advertise OAuth. Also `FetchAuthServerMetadata` and `RevokeToken` (RFC 7009). Verify: httptest fixtures for path-suffixed metadata (the Atlassian shape), origin-only metadata, non-OAuth 401, 200.
- [x] 2.3 Classification in `connect` (design D5): an `OAuthAuthorizationRequiredError` from the OAuth transport → `*AuthRequiredError` (nil Discovery); a failed HTTP/SSE dial/handshake in none mode whose `Discover` **succeeds** → `*AuthRequiredError{Discovery}`; everything else unchanged. `errors.Is(err, mcp.ErrAuthRequired)` holds for both. Verify: tests for no-token, expired-token-refresh-rejected, none-mode 401 with valid metadata, none-mode 401 without metadata (stays error), server down (stays error).
- [x] 2.4 `mcp.StartAuthorization` (design D2/D4): register via DCR when `ClientID` is empty (`RegisterClient`, read back `GetClientID/GetClientSecret`), bind `DefaultRedirectURI`'s port with an ephemeral fallback that re-registers, PKCE + state via mcp-go generators, `GetAuthorizationURL`, serve `/callback` once (success page, `ProcessAuthorizationResponse` saves tokens into `Tokens`), 5-minute timeout; returns `Authorization{URL, ClientID, ClientSecret, RedirectURI, Done <-chan error, Cancel func()}` immediately. Verify: end-to-end test against an httptest authorization server (mcp-go's `client/transport/oauth_test.go` is the reference): DCR → URL → simulated redirect → token saved; state mismatch rejected; timeout path; port-taken fallback.

## 3. Host seams and composition

- [x] 3.1 `services.URLOpener` + `NoBrowser` (`urlopener.go`), `EventMCPOAuthCompleted` + payload (`events.go`), `SettingsService` fields `emitter` (default `Noop{}`) and `urlOpener` (default `NoBrowser{}`) with `WithEmitter`/`WithURLOpener` options. Verify: `go test ./internal/services/...` still green; option tests.
- [x] 3.2 `cli.Assemble(ctx, emitter, opener)` threads the opener and emitter into `NewSettingsService`; `NewRootCommand`/`newServeCommand` carry it; desktop `main.go` implements the opener with Wails `app.Browser.OpenURL` (same atomic-pointer pattern as `wailsEmitter`) and registers `MCPOAuthCompletedEvent`; `main_server.go` passes `services.NoBrowser{}` (design D8). Verify: `go build ./... && go build -tags server ./... && go test ./internal/cli/...`.

## 4. Frontend: row controls (no new bindings needed)

- [x] 4.1 McpPane rows lead with a `StatusDot` (connected → success, error → danger, `auth_required` → warn, else neutral) beside the status chip; `auth_required` chip reads "authorization required" on the warn tint and its reason is warn-tinted, not danger. Verify: McpPane tests assert the dot status per state.
- [x] 4.2 Row-level enabled `Toggle` (design D7): off → `updateMCPServer({…, Enabled:false})` and swap the row; on → update then probe the row. The dialog toggle stays. Verify: tests for both directions (update called with the flipped flag; probe called only on enable).
- [x] 4.3 Probe → **Refresh** per row; pane-level **Refresh all** fans `probeMCPServer` over enabled servers with per-row loading state (`Promise.allSettled`). Verify: tests — refresh-all probes only enabled rows and updates each.

## 5. Services: probe classification, authorization flow, disconnect

- [x] 5.1 `agents.NewMCPTokenStore` adapter (`store.ErrNotFound` → `mcp.ErrNoToken`, JSON round-trip of `mcp.Token`); `agents.MCPServerRef(server, tokens)` hydrates `Conn.OAuth` from `server.Auth` when `IsOAuth()`; `resolveMCPTools` and tests updated. Verify: `go test ./internal/agents/...`.
- [x] 5.2 `ProbeMCPServer` = invalidate, then probe (design D6); `errors.Is(err, mcp.ErrAuthRequired)` persists `auth_required` with a human message (none-mode discovery: "requires OAuth — Authorize to enable it"; OAuth mode: "authorization required — Authorize to sign in"); definition untouched. Verify: service tests with a fake store.
- [x] 5.3 `AuthorizeMCPServer(ctx, id) error` (design D2/D4/D8/D9): bounded `Discover`; flip `Auth.Mode` to oauth; reuse the stored registration when the issuer matches (clear it when it changed); `StartAuthorization` with the token-store adapter; persist registration (`ClientID/ClientSecret/RedirectURI/Issuer/ResourceMetadataURL/AuthServerMetadataURL/Scopes`); `urlOpener.Open` — on `ErrNoBrowser` cancel the flow, persist `auth_required` + the message, return the error; completion goroutine: success → `ProbeMCPServer` then emit `{ok:true}`; failure/timeout → persist `auth_required` + reason, emit `{ok:false, error}`. One in-flight authorization per server. Verify: service tests with a fake opener + fake emitter + httptest AS.
- [x] 5.4 `DisconnectMCPServer(ctx, id) (store.MCPServer, error)` (design D9): best-effort revoke at the AS `revocation_endpoint` when advertised, delete tokens, invalidate, persist `auth_required` ("disconnected — authorize to reconnect"), keep the registration. Verify: tests — revoke called when advertised, skipped when not, tokens deleted, auth kept.

## 6. Frontend: OAuth actions and bindings

- [x] 6.1 Regenerate Wails bindings (`task common:generate:bindings` or `task build`); `client.ts` gains `authorizeMCPServer`, `disconnectMCPServer`, and `events.onMcpOAuthCompleted`; `events.ts` gains the constant. Verify: `tsc` clean.
- [x] 6.2 Dialog: **Authentication** `Select` (none / OAuth 2.1) for http and sse transports writing `Auth.mode`; `emptyServer()` includes `Auth`. Row: **Authorize** on `auth_required` rows (calls `authorizeMCPServer`, row unchanged until completion); **Disconnect** on OAuth-mode connected rows (swaps the row for the returned state). Subscribe to `mcp:oauth:completed` → refetch that row; surface `ok:false` errors inline. Verify: McpPane tests for the select, both actions, and the event-driven refresh.

## 7. Gates

- [x] 7.1 `gofmt -w .`, `go vet ./...`, `go test ./...` (`-race` on store/mcp/services), `go build -tags server ./...`, `task build`, `vp check src`, `npm test` — all green; bindings regenerated after the final `task build` (stale-dist gotcha). Verify: fresh output recorded in `.team/handoffs/`.
