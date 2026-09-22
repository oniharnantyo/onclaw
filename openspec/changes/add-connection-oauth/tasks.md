## 1. Domain & Storage

- [x] 1.1 Add `oauth` auth kind to recipe descriptors: authorize endpoint, token endpoint, scopes per access level, refresh margin, app-registration guidance
- [x] 1.2 Add connection token-lifecycle fields (refresh envelope, expires at, granted scopes) and the `expired` connection status; extend validation and status transitions
- [x] 1.3 Migration: token-lifecycle columns on `workspace_connections` and instance OAuth apps table (provider-unique), with down migration
- [x] 1.4 Instance OAuth apps store port + postgres adapter + fakes; client secret encrypted with the instance-scoped key derivation, write-only with last-4 hint

## 2. OAuth Flow Service

- [x] 2.1 Authorize URL builder: recipe endpoints + registered app + HMAC single-use signed state (workspace, user, nonce, short TTL)
- [x] 2.2 Callback handler: validate state (replay/cross-workspace/unknown rejected before any exchange), exchange code at the recipe's token endpoint, store token set, run probe, activate or discard-on-failure
- [x] 2.3 Refresh-on-resolution wrapper: within-margin refresh at credential resolution, write-through into the materialized server's `Authorization` row via the MCP settings service, failure sets `expired` with provider error
- [x] 2.4 Reauthorize action: new consent flow replacing the token set in place (connection id, server, attachments preserved)
- [x] 2.5 Gallery availability logic: OAuth recipe is available iff its provider app is registered; missing-app connect attempts fail with a naming error
- [x] 2.6 Instance admin service: register/update OAuth apps per provider, encrypted secret storage, redirect URI derivation from instance public base URL

## 3. HTTP API & Wiring

- [x] 3.1 Routes: connect (returns authorize redirect for OAuth recipes), OAuth callback (public, state-validated), reauthorize, instance OAuth app CRUD (instance admin guard); management verbs under `domain.IntegrationsWrite`
- [x] 3.2 Error translation: missing app registration, state rejection, exchange failure, probe failure (store-nothing envelope)
- [x] 3.3 Composition root wiring: OAuth service dependencies injected granularly; no nil-defaulted seams

## 4. Web

- [x] 4.1 Connect dialog OAuth variant: explains the consent hand-off, redirects; callback return path lands on the gallery with the connection active or the failure surfaced
- [x] 4.2 Connected cards: `expired` status rendering with Reauthorize action and provider error
- [x] 4.3 Instance admin OAuth apps pane: provider rows, client id/secret entry (secret write-only, last-4 hint after save), redirect URI display with copy, per-provider status
- [x] 4.4 Design-contract conformance: tokens, states, responsive 360–1920

## 5. Verification

- [x] 5.1 Unit tests: state signing/replay rejection, exchange+probe gating (store-nothing on failure), refresh margin/write-through, expiry transition, reauthorization preserving attachments
- [x] 5.2 HTTP tests: guards (IntegrationsWrite on connect/reauthorize; instance admin on app CRUD), public callback validation errors
- [ ] 5.3 Recipe live-verification: Atlassian app registration + consent + probe against a real site (reference recipe); Slack and Linear recipes declared and verified at apply time
- [x] 5.4 `go build ./...`, `go vet ./...`, `go test ./...` green; web typecheck + tests green
- [x] 5.5 Smoke coverage: stub-provider OAuth connect → refresh → expire → reauthorize lifecycle
- [ ] 5.6 Manual pass: real Atlassian connect end-to-end, expiry + Reauthorize recovery, expired connection's effect on attached agent runs
