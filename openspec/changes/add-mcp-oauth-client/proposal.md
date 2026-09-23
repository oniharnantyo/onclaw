## Why

Every SaaS remote MCP server we live-probed — GitLab, Notion, Linear, Slack, Stripe, Asana, Sentry — is an OAuth 2.0 protected resource advertising RFC 9728 protected-resource metadata, and OnClaw cannot connect to any of them: MCP connections support only static headers, and the instance-app OAuth flow (built for connection recipes) requires a per-provider confidential app with a mandatory client secret and no PKCE. GitLab's recipe is outright broken (PAT against an OAuth-only endpoint), and Notion/Sentry-class servers demand full MCP-spec OAuth. One standards-correct OAuth client at the MCP connection layer unlocks the entire family with zero per-provider admin registration.

## What Changes

- MCP connections (workspace and agent servers) gain an **auth mode**: `none` (today's static headers, unchanged default) or `oauth`.
- New MCP OAuth client implementing the spec-authorization chain: `WWW-Authenticate` → RFC 9728 PRM discovery (including path-inserted well-knowns, e.g. GitLab's `/.well-known/oauth-protected-resource/api/v4/mcp`) → RFC 8414 authorization-server metadata → client identification via **OAuth Client ID Metadata Document** with **RFC 7591 DCR** as fallback → authorization-code + **PKCE S256** → bearer use + refresh with **RFC 9207 `iss` validation**.
- **Bring-your-own app variant** for servers without DCR (pre-registered client id + secret, or public client + PKCE) — the instance-app pattern lifted to the MCP layer, per server rather than per provider.
- **Headless flow**: when the instance has no public base URL, servers advertising `device_authorization_endpoint` connect via the RFC 8628 device-code grant with a paste-back UI.
- Tokens stored encrypted (AES-256-GCM envelope, same derivation as existing secret rows), refreshed on-dial within a margin with fail-open semantics matching `RuntimeCredentialSource`; a failed refresh surfaces the connection's `expired` status convention on the server row.
- **Launch presets**: Notion (`mcp.notion.com/mcp`, DCR live, scope `default`) and Sentry (`mcp.sentry.dev/mcp/{org}`, OAuth) ship as pre-filled server templates in the add-server UI — both connect with zero admin registration.

## Capabilities

### New Capabilities

- `mcp-oauth`: OAuth protection for MCP connections — discovery chain, client registration strategies (metadata document / DCR / BYO), browser and device authorization flows, token storage and refresh lifecycle, dial-time bearer injection, and the launch presets.

### Modified Capabilities

- (none — `workspace-mcp`'s existing requirements describe transport/static-header behavior, which is unchanged; the auth mode is new surface owned by `mcp-oauth`)

## Impact

- New `internal/agents/mcp/oauth` package (discovery, registration, PKCE, device flow, token lifecycle)
- `internal/agents/mcp/client.go` (dial: bearer injection; 401-driven discovery kick-off)
- `internal/domain/mcp.go` (connection auth-mode fields + validation), store sub-interface + migration for the token row (postgres + fake)
- `internal/server` (authorize begin/callback routes bound to server rows; state machinery reused from the connections OAuth service), status surfacing
- `internal/services/connections_oauth.go`-adjacent seam: reuse of sealed-state, nonce, and `DeriveOAuthRedirectURI` helpers (extracted to a shared spot rather than duplicated)
- Web: server rows show sign-in/re-authorize affordance and an expired chip; add-server dialog gains the two presets
- No change to the connections/recipes OAuth path (instance-app flow stays as-is)
