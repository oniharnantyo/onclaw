## 1. Attachment service (design D1)

- [x] 1.1 Add `WithAttachmentTx` functional option to `ConnectionsService` (narrow tx seam: `run func(ctx, store.AgentStore) error`) and the composition-root adapter over `store.Store.WithTx`
- [x] 1.2 Implement `SetAttachedAgents(ctx, workspaceID, connectionID, agentIDs)`: load connection, resolve attach id per kind (MCP → `GetByOriginConnection` server id, http → connection id), validate every submitted agent id exists in the workspace, diff against current attachment
- [x] 1.3 Apply the diff inside the tx seam with per-agent tx-scoped read-modify-write of `enabled_mcps` (touch only the connection's id in each array); atomic all-or-nothing
- [x] 1.4 Service tests (fake store): multi-attach save, detach-keep-others, unknown agent id rejects with nothing changed, http-kind stores the connection id, idempotent re-save

## 2. Token replacement service (design D2)

- [x] 2.1 Implement `ReplaceToken(ctx, workspaceID, connectionID, token)`: OAuth-kind refused via sentinel error naming reauthorization; empty token rejected as invalid (the keep-semantics live at the endpoint/dialog layer, D3)
- [x] 2.2 Candidate-credential probe before any write: MCP kind dials `probeServer` with the new token composed into an ad-hoc `MCPConnection`; http kind reruns the recipe probe call with the new token
- [x] 2.3 On success: MCP kind rewrites the recipe's secret row via `WorkspaceMCPSecretStore` (non-empty merge-on-name); http kind gains an envelope-update path beside `connectHTTP`; identity, origin, access level, status, and attachments untouched; failure stores nothing
- [x] 2.4 Service tests: successful replace keeps attachments and refreshes the last-4 hint, failed probe keeps the stored token and surfaces the upstream message, OAuth-kind refusal, http-kind envelope replace

## 3. HTTP surface (design D3/D5)

- [x] 3.1 `PUT /integrations/connections/:id/agents` handler + route behind `RequirePermission(domain.IntegrationsWrite)`; 404 for unknown/cross-workspace connection, 422 for unknown agent ids
- [x] 3.2 `POST /integrations/connections/:id/token` handler + route behind the same guard; OAuth-kind sentinel maps to an envelope directing to reauthorization; probe failure passes the upstream message through
- [x] 3.3 Handler tests: permission rejection for a Member, envelope shapes for both error paths, success payloads returning the refreshed `ConnectionView`

## 4. Web API + shared selection component (design D4)

- [x] 4.1 `connectionsApi.setAgents(ws, connectionId, agentIds)` and `connectionsApi.replaceToken(ws, connectionId, token)`
- [x] 4.2 Extract `AttachAgentsList` (workspace agents × toggles, optimistic rows, per-agent error toast, `data-testid="attach-agents-list"` + per-row testids) and rewire the PAT connect hand-off in `ConnectServiceDialog` onto it; component tests

## 5. Edit dialog + entries (design D4)

- [x] 5.1 `ConnectionEditDialog`: attached-agents list, token field (token-auth/http kinds only; OAuth kinds render Reauthorize instead), display-only origin + access-level chips, save ordering token-then-agents with all-or-nothing error handling and a "Verifying…" save state; component tests
- [x] 5.2 Integrations card gains an Edit action (writer-gated) opening the dialog; the read-only agents line stays; section tests
- [x] 5.3 OAuth callback success (`?oauth=…&status=connected`) auto-opens the edit dialog for the activated connection; section tests
- [x] 5.4 Responsive check at the 360px column (agent rows and chips wrap without horizontal overflow)

## 6. Verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` and postgres integration tests green
- [x] 6.2 `npx tsc -b` and web vitest suite green
- [x] 6.3 Smoke section: connect a mock PAT service → `PUT agents` reflects in `attached_agents` → `POST token` with a bad token keeps the old one and a good one swaps it; suites stay green
- [x] 6.4 `openspec validate add-connection-edit --strict` clean
- [ ] 6.5 Live pass (user-gated): edit attachments on a real connection and replace its token against the live provider
