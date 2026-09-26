## Why

A connection is workspace-level and already attaches to any number of agents, but the flows only expose that flexibility one agent at a time: the OAuth connect flow offers no attach step at all (the consent redirect bypasses the PAT flow's agent hand-off), the connection card shows attached agents as read-only text, and a rotated PAT can only be installed by disconnecting — which strips the attachment from every agent — and re-attaching each agent by hand.

## What Changes

- Connection-centric attachment management: an Edit surface on a connected card (and reused at connect completion) lists the workspace's agents with per-agent toggles; saving applies the full desired agent set in one atomic server-side diff over the affected agents' `enabled_mcps` — for both connection kinds (materialized server id and raw connection id).
- Connect completion gains the agent-selection step for both auth kinds: PAT keeps its hand-off list; an OAuth connect that returns successfully lands on the same selection surface for the new connection.
- PAT/http-kind token replacement: a connection card accepts a replacement token that is probe-gated (the stored token survives a failed probe), swapped in place, and never disturbs attachments. OAuth-kind connections keep reauthorization as their only credential-rotation path and present no token field.
- New endpoints behind `integrations.write`: `PUT /integrations/connections/:id/agents` (atomic attachment diff) and `POST /integrations/connections/:id/token` (probe-gated replacement).
- No schema change: attachment already lives in agent rows; replacement rewrites the existing secret rows. The one-connection-per-(workspace, service, origin) uniqueness is unchanged — per-agent credentials remain out of scope, and access level stays display-only after connect.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `workspace-connections`: adds connection-centric attachment management (atomic multi-agent apply, offered at connect completion for both auth kinds) and probe-gated PAT/http-kind token replacement; both ride `integrations.write`.

## Impact

- Backend: `internal/services/connections.go` (attachment diff service method, replace-token service method), `internal/server/handlers/connections.go` + route registrations in `internal/server/router.go` (two new endpoints under the existing permission guard), reusing the existing agent store patch path — no migration, no new stores.
- Web: `IntegrationsSection.tsx` (Edit action + dialog), `ConnectServiceDialog.tsx` (shared attach list; OAuth callback landing opens the selection surface), `connectionsApi.ts` (two new calls).
- Specs: `workspace-connections` delta only. The seven existing connection changes stay the authority on connect/probe/refresh/reauthorize/disconnect semantics; nothing in them is modified.
