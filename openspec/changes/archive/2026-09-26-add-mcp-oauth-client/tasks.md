## 1. Shared OAuth-state extraction

- [x] 1.1 Extract sealed-state, nonce, and `DeriveOAuthRedirectURI` helpers from `services/connections_oauth.go` into a shared package; connections service delegates; existing tests green

## 2. Domain + store: auth mode and token rows

- [x] 2.1 `domain/mcp.go`: auth-mode constants + fields on `MCPConnection` (mode, client-id/secret rows for BYO), validation (oauth ⇒ URL transport; BYO rows validated like headers); static mode byte-identical
- [x] 2.2 Token store sub-interface (create/get/update/delete by scope kind + server id) with encrypted access/refresh envelopes, expiry, scopes, issuer; migration 000068 up/down; fake-store implementation
- [x] 2.2b Persist auth-mode/BYO columns on server rows (migration 000069)
- [x] 2.3 Server-row status gains `expired` (entered only by refresh failure, left only by reauthorization) with transition tests mirroring the connection lifecycle rules

## 3. Discovery chain

- [x] 3.1 Implement `WWW-Authenticate` parsing (`resource_metadata`) and RFC 9728 well-known resolution incl. path-inserted form; RFC 8414 AS metadata fetch (path-insertion per RFC 8414); in-memory TTL cache keyed by server URL
- [x] 3.2 Table tests against recorded fixtures (gitlab.com challenge + PRM, notion PRM + AS metadata with DCR, a no-metadata server)
- [x] 3.3 Discovery failure → errored status with guidance detail; no token requests attempted

## 4. Client strategies + grants

- [x] 4.1 BYO strategy: configured client id/secret or public client; secret only to token endpoint
- [x] 4.2 Client ID Metadata Document strategy: fetch/validate client metadata document when the client id is an HTTPS URL
- [x] 4.3 RFC 7591 DCR strategy against `registration_endpoint`; persist registration result per server row (register once)
- [x] 4.4 Authorization-code + PKCE S256 flow: sealed state mint/validate, redirect URI derivation, code exchange, `iss` validation (RFC 9207) on token responses
- [x] 4.5 RFC 8628 device flow: device authorization request, poll loop with interval growth + max duration + cancellation, paste-back payload for the UI
- [x] 4.6 Refresh within margin; failed refresh fails open (stored token used) and persists `expired` + provider error

## 5. Dial integration

- [x] 5.1 `client.go` dial: bearer injection for oauth-mode connections; needs-authorization signal surfaced as status detail (not a run failure); probe/run parity preserved
- [x] 5.2 Extend `RuntimeCredentialSource`: workspace and agent-private resolution refresh within margin (agent path currently delegates untouched — wrap it too)
- [x] 5.3 Reauthorization path: begin (new state) + callback clears `expired`; token rows replaced atomically

## 6. HTTP surface

- [x] 6.1 Authorize-begin + callback routes bound to server rows (workspace + agent scopes, permission-gated like server config); state claims carry scope kind + server id
- [x] 6.2 Device-flow begin/status endpoints for headless instances (no public base URL)
- [x] 6.3 Status/detail surfacing on server rows (needs-authorization, expired, discovery/registration errors)

## 7. Web

- [x] 7.1 Server rows: sign-in / re-authorize affordance, expired chip, needs-authorization state with action
- [x] 7.2 Add-server dialog: auth mode field (none/oauth), BYO client rows when oauth, device-flow paste-back screen
- [x] 7.3 Launch presets: Notion and Sentry templates (URL, transport, oauth mode) with provider help text

## 8. Verification

- [x] 8.1 `go build ./... && go vet ./... && go test ./...` green; web typecheck/tests green; `openspec validate` clean
- [ ] 8.2 Live pin: Notion preset end-to-end in the dev instance (DCR + browser authorize + tools listed)
- [ ] 8.3 Live pin: GitLab.com OAuth-mode server (DCR, scope `mcp`) lists tools — retires the broken PAT recipe path for good
- [x] 8.4 Headless device flow exercised with a provider advertising `device_authorization_endpoint` (or fixture-simulated if none reachable) — fixture-simulated: 8 oauth-package tests + handler `TestMCPOAuthDeviceFlowHappyPath`; no reachable provider advertised `device_authorization_endpoint` (probed Notion/GitLab/Sentry 2026-09-24)
- [x] 8.5 Smoke suite green (866/866, fresh onclaw_smoke DB); update the explore memory pointer after apply
