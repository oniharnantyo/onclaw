## Context

`MCPConnection` carries transport + static rows only; `client.go:dial` attaches `headerMap(conn.Headers)`. Live probes (2026-09-23, recorded in the explore memory) show the target universe speaks one pattern: 401 + `WWW-Authenticate` with `resource_metadata` (GitLab, GitHub, Sentry) or plain 401 (Atlassian) / PRM well-knowns (Notion, Linear, Slack, Stripe, Asana); Notion additionally shows DCR + confidential + public-PKCE clients all supported (`token_endpoint_auth_methods_supported` includes `none`), GitLab DCR live at `/oauth/register`. The connections OAuth service already owns proven machinery this change should reuse, not duplicate: HMAC-sealed single-use state, nonce consumption, `DeriveOAuthRedirectURI`, AES-256-GCM secret envelopes, refresh-within-margin with fail-open dial semantics (`RuntimeCredentialSource`), and the `expired` status convention. Hermes validates the design and adds the ordering lesson: client identification tries the Client ID Metadata Document before falling back to DCR.

## Goals / Non-Goals

**Goals:**
- One client covering the probed universe: GitLab (DCR), Notion (DCR), Sentry (OAuth), and BYO-app servers.
- Zero per-provider admin registration for the launch presets.
- Reuse the state/nonce/redirect/encryption/refresh-fail-open machinery; extract shared helpers rather than copy.

**Non-Goals:**
- Migrating the connections/recipes OAuth flow (instance-app per provider stays as built).
- Atlassian's MCP server (served by the existing connection recipe's instance-app flow; its server predates PRM).
- Sampling, elicitation, mTLS, tool filtering (later waves from the Hermes read).
- Auto-retrying registration across restarts; background token refresh loops (refresh stays on-dial, matching the shipped convention).

## Decisions

- **D1: New package `internal/agents/mcp/oauth`, wired at the dial seam.** `dial` gains an auth step before `Start`: if mode=oauth and no usable token → kick discovery (cached per server URL) and return a "needs authorization" signal that surfaces as the server's status (a distinct status detail, not a run failure). Bearer injection joins the header map at dial time. Rationale: keeps the probe/run parity the runtime already guarantees; the manager's idle-reap means re-dials pick up fresh tokens naturally.
- **D2: Discovery cache keyed by server URL, TTL-bounded.** PRM/AS metadata is fetched once and cached (in-memory, e.g. 1h) with the selected authorization server + scopes; the 401-challenge path can refresh it. Rationale: discovery adds RTTs to every cold dial otherwise; metadata is stable.
- **D3: Client strategy order — BYO → CIDM → DCR.** Pre-registered credentials in the server config win (deterministic, enterprise-friendly: GitLab admin-disabled DCR is a documented mode); otherwise a client-id metadata URL if configured/self-hostable; otherwise DCR. PKCE S256 whenever advertised; confidential clients send the secret only to the token endpoint. Rationale: Hermes ships the same order; BYO also covers Asana-V2-class servers with no DCR.
- **D4: Token row, not secret-row overload.** New store rows keyed by (scope kind, server id) holding encrypted access/refresh envelopes, expiry, granted scopes, issuer — separate from user-visible header rows so the UI's secret-row model (hint/last-4, manual edit) stays meaningful while refresh mutates tokens invisibly. Migration 000068+; fake-store parity for tests.
- **D5: State/device-code reuse.** Extract the connections OAuth service's sealed-state + nonce + redirect-URI helpers into a small shared package (e.g. `internal/auth/oauthstate`) consumed by both flows; callback routes live under the existing workspace/agent MCP server routes with the server id bound into the state claims. Device flow: server polls with RFC 8628 intervals; UI shows URI + code for paste-back; polling capped (interval growth + max duration), cancellation propagates.
- **D6: Status conventions extended, not reinvented.** Server rows reuse `connected`/`error` and gain `expired` semantics mirroring the connection lifecycle: entered only by refresh failure (fail-open dial still proceeds), left only by reauthorization. `StatusError` carries the provider's error detail.
- **D7: Agent-private servers ride the same path.** `AgentServersForRuntimeByID` gets the same refresh-on-resolution wrapper the workspace path has (today it delegates untouched since agent servers had no OAuth) — probe/run parity holds for private servers.

## Risks / Trade-offs

- [Provider divergence in the discovery chain (Atlassian-style servers without PRM)] → discovery failure is a clean error with guidance to use BYO/static modes; presets only ship for verified servers.
- [DCR rate limits (GitLab 10/hr/IP) bite on busy instances] → registration result is persisted per server row (register once, reuse forever); BYO documented in guidance.
- [Token theft surface grows with stored refresh tokens] → AES-256-GCM envelopes under the instance master key (same as refresh ciphertexts today), presence-only reads, tokens never logged; device flow codes short-lived.
- [Shared-state extraction touches the shipped connections OAuth service] → extraction is mechanical (move + delegate), covered by existing tests; land it as the first task.

## Migration Plan

Migration 000068+ (token rows) up/down. No behavioral change for existing servers (auth mode defaults to `none`). Deploy order: backend, then web presets. Rollback: revert; token rows are ignored by old code.

## Open Questions

- Sentry's exact scope strings (its docs don't list them; PRM/AS metadata at apply time pins them) — presets carry empty scope sets where the server defines defaults, so this doesn't block the spec.
