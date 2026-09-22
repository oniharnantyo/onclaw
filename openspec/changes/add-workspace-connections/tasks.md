## 1. Domain & Storage

- [x] 1.1 Add connection domain types: connection entity (id, workspace, service, access level, timestamps), access-level values (`read_only`, `read_write`), validation (service non-empty, access level in catalog), and error sentinels (unknown recipe chains to ErrInvalid; duplicate per-service chains to ErrConflict)
- [x] 1.2 Add recipe descriptor types (service id/name/icon, auth kind, availability, transport endpoint, guided steps, recommended scopes per access level, probe declaration) and the in-Go recipe registry: GitHub and GitLab available (GitLab targeting gitlab.com), Atlassian (single recipe covering Jira and Confluence), Slack, and Linear declared coming-soon
- [x] 1.3 Migration 000062: create `workspace_connections` table (unique on workspace_id + service), add nullable indexed `origin_connection_id` to the workspace MCP servers table, and backfill `integrations.write` into existing workspaces' built-in Owner and Admin role rows plus the master tenant's Superadmin role row — all with down migration
- [x] 1.4 Add connection store port (create, get, list by workspace, get by service, delete) and postgres adapter; extend workspace MCP server store with origin-link lookup; update the in-memory fake
- [x] 1.5 Cascade delete through the store transaction seam: deleting a connection removes its linked server row and the server's agent attachment references atomically
- [x] 1.6 Add `integrations.write` to the closed permission catalog (`internal/domain/permissions.go`): constant, Owner and Admin default sets (Superadmin inherits via its all-permissions set), Member untouched

## 2. Connections Service

- [x] 2.1 Implement the connections service: connect (resolve recipe → validate access level + token non-empty → build materialized server with recipe endpoint and `Authorization` secret row via the MCP settings secret machinery → probe before persist → persist connection + server on success), honoring one-connection-per-service conflict
- [x] 2.2 Implement connect-failure hygiene: probe failure stores nothing (no connection row, no token ciphertext) and returns the probe error
- [x] 2.3 Implement list/get (joining recipe descriptors with connection state, materialized server status, attached agent names, and hint-only token display) and disconnect (D8 cascade)
- [x] 2.4 Implement probe-on-demand: re-run the recipe probe against the linked server and persist status through the existing probe path
- [x] 2.5 Wire recipes endpoint: return the registry with availability (available / coming_soon); coming_soon recipes accept no connect requests

## 3. HTTP API

- [x] 3.1 Add REST endpoints under the workspace scope: GET/POST connections, GET/DELETE connection, POST connection probe, GET recipes; management verbs guarded with `domain.IntegrationsWrite`, reads riding membership
- [x] 3.2 Error translation: conflict (duplicate service), invalid (unknown recipe, bad access level), probe-failure envelope carrying the upstream message, and the managed-server pointer error (origin-marked servers reject edit/delete naming the owning connection; probe stays allowed)
- [x] 3.3 Composition root wiring: connections service + handlers registered with granular stores; no nil-defaulted dependencies

## 4. Web

- [x] 4.1 Integrations settings tab: gallery from recipes (available cards with Integrate, coming-soon cards disabled with truthful copy, Custom MCP card linking to existing MCP management) and the Connected section (status, access level, attached agents, last-4 hint, manage/disconnect)
- [x] 4.2 Connect dialog per recipe: guided token-creation steps with recommended scopes, access level preselected to read-only, token input, probe-gated Connect button with loading/error states, success hand-off offering agent attachment
- [x] 4.3 Disconnect confirmation with cascade consequences stated; conflict and permission errors surfaced
- [x] 4.4 Agent config Integrations section: managed connections listed as attachable entries over the existing agent MCP attach endpoints, showing access level per attachment
- [x] 4.5 Design-contract conformance: design tokens, states (hover/focus/loading/empty/error/success), monospace chips for services/models, responsive 360–1920 with no horizontal overflow

## 5. Verification

- [x] 5.1 Store + service unit tests against fakes: connect happy path, probe-gate rejection stores nothing, duplicate conflict, unknown recipe, disconnect cascade, hint-only reads
- [x] 5.2 HTTP-level tests: permission matrix (Member rejected; custom role with tools.write but without integrations.write rejected; Owner/Admin/Superadmin allowed), managed-server pointer errors (edit/delete rejected, probe allowed, hand-made unaffected), error envelopes
- [ ] 5.3 Recipe live-verification (GitHub, GitLab): confirm endpoint URLs and transports (GitLab: hosted remote endpoint vs official stdio server — prefer remote; if stdio, document the image-provisioning requirement), auth header/env shapes, and a probe tool call succeed with real PATs; pin the verified values into the recipes
- [x] 5.4 `go build ./...`, `go vet ./...`, `go test ./...` green; web typecheck + tests green
- [x] 5.5 Smoke test coverage: connection lifecycle end-to-end (create → status → attach agent → disconnect cascade) in `scripts/smoke.sh`
- [ ] 5.6 Manual pass: gallery at 360px and 1920px, connect flow with a real token, disconnected agent behavior after cascade (tools gone, no run failure)
