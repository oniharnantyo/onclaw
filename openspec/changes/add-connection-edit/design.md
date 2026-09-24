## Context

A connection is a workspace-level credential row; attachment is each agent's `enabled_mcps` array holding the materialized server id (MCP kind) or the connection id (http kind). Nothing in the data model limits attachment to one agent, but every flow that touches attachment is per-agent or absent: the agent config Integrations section toggles one agent at a time, the PAT connect hand-off toggles agents over per-agent PATCHes (optimistic, non-atomic), the OAuth callback activates the connection with no attach step, and a rotated PAT requires disconnect (which cascades every attachment away) plus manual re-attach. See proposal.md — Why.

Existing seams this design reuses: `ConnectionView.ServerID` and the `buildHTTPView`/`attachedAgentNames` machinery for kind-aware attach ids; `probeServer(ctx, ws, id, name, MCPConnection)` dials an ad-hoc candidate credential without persisting anything (connect uses it pre-persist); `WorkspaceMCPSecretStore` merge-on-name secret semantics; `store.Store.WithTx` as the documented transaction seam (handlers use it today — `handlers/members.go`).

## Goals / Non-Goals

Goals:

- One connection-centric surface that sets the full agent set atomically and replaces a PAT/http-kind token in place.
- Connect completion offers the same agent selection for both auth kinds.
- All-or-nothing dialog save: token failure leaves attachment untouched.

Non-Goals:

- Per-agent credentials — the one-connection-per-(workspace, service, origin) uniqueness stands; every attached agent shares one token.
- Post-connect access-level changes (read_only ↔ read_write is re-consent/new-scope territory; the field stays display-only).
- Origin changes (already immutable by design; not touched here).
- Background re-attachment or auto-attach policies (e.g. "all future agents").

## Decisions

### D1: Server-side atomic attachment diff over a narrow tx seam

`ConnectionsService.SetAttachedAgents(ctx, workspaceID, connectionID, agentIDs []string)` validates the connection, resolves the attach id per kind (MCP kind → `GetByOriginConnection`, http kind → the connection id — the same resolution `buildView` performs), validates every submitted agent id exists in the workspace, diffs against current attachment, and applies inside one transaction.

The transaction reaches the service through a narrow injected seam, not the whole aggregate: a functional option `WithAttachmentTx(run func(ctx context.Context, agents store.AgentStore) error) error` whose implementation at the composition root adapts `store.Store.WithTx` and hands the closure the tx-scoped `AgentStore`. The service applies read-modify-write per affected agent **inside** the tx (re-reading each agent's array and editing only the connection's id), so a concurrent MCP-server opt-in cannot be clobbered by a stale array write.

- Alternatives: (a) web-side N× agent PATCH — rejected: non-atomic, partial failure states, already fragile in the connect hand-off; (b) handler-level `WithTx` (the `members.go` precedent) — rejected here because kind resolution, unknown-agent validation, and diff logic are service rules and would split across layers; (c) a new `store.Connections.SetAttachedAgents` spanning agent rows — rejected: cross-aggregate store method.

### D2: Probe-gated in-place token replacement

`ConnectionsService.ReplaceToken(ctx, workspaceID, connectionID, token)` loads the connection, rejects OAuth-kind connections with a sentinel error mapped to an envelope that names reauthorization, then probes the **candidate** credential exactly the way connect does — `probeServer` with the new token composed into an ad-hoc `MCPConnection` for MCP kind, the recipe probe call for http kind — before any write. On success: MCP kind rewrites the recipe's secret header/env row through `WorkspaceMCPSecretStore` (non-empty merge-on-name replace; the managed-server immutability rule bars the MCP surfaces, not the owning connection); http kind rewrites the connection's encrypted envelope column (new update path beside `connectHTTP`'s create). Identity, origin, access level, status (an `expired` http/MCP status clears on successful replace only where the existing status rules allow it — replace never manufactures `connected` for OAuth states), and all attachments are untouched. Failure stores nothing.

### D3: Two endpoints, dialog-ordered all-or-nothing

`PUT /integrations/connections/:id/agents` `{agent_ids: []}` and `POST /integrations/connections/:id/token` `{token}`, both behind `RequirePermission(domain.IntegrationsWrite)` next to connect/disconnect. Two endpoints instead of one combined save because they have different failure shapes (fast diff vs. seconds-long provider probe); the edit dialog composes them — token first, then agents — so a failed probe aborts the whole save and the UX stays all-or-nothing. An empty token field simply skips the token call, which is what makes "empty submission keeps the stored token" free.

### D4: One shared agent-selection component, three entries

A shared `AttachAgentsList` (workspace agents × toggles, optimistic row state, per-agent error toast) is rendered by: (1) the existing PAT connect hand-off inside `ConnectServiceDialog` (replacing its inline list), (2) the new `ConnectionEditDialog` opened from the card's Edit action, and (3) the same dialog auto-opened when the OAuth callback lands with `status=connected` (the gallery already intercepts the callback params; it resolves the fresh connection by recipe id and opens edit mode). The edit dialog shows origin and access level as display-only chips (immutable), the token field for token-auth/http kinds, and a Reauthorize button instead of the token field for OAuth kinds.

```
┌─ Edit GitLab ──────────────────────────────────────────────┐
│  ATTACHED AGENTS                                           │
│  ┌──────────────────────────────────────────────┐          │
│  │ Atlas                                    [◉ on] │          │
│  │ Beacon                                   [○ off]  │          │
│  │ Cronus                                  [○ off]  │          │
│  └──────────────────────────────────────────────┘          │
│  Agents gain or lose its tools on their next run.          │
│                                                            │
│  REPLACE ACCESS TOKEN                              (PAT only)  │
│  ┌──────────────────────────────────────────────┐          │
│  │ ··3f9a   (paste a new token to replace)      │          │
│  └──────────────────────────────────────────────┘          │
│  Leave empty to keep the stored token. Verified with a     │
│  check before it replaces the old one.                     │
│                                                            │
│  Origin  gitlab.linkaja.com   Access  Read-only   ← display-only │
│                              [Cancel]  [Save changes]      │
└────────────────────────────────────────────────────────────┘
```

(Approved shape from the explore gallery. OAuth-kind variant: token field replaced by a Reauthorize button; http-kind identical to PAT plus the verbs collapsible stays connect-only.)

### D5: Permission tier unchanged

Both operations are credential-bearing or attachment-bearing writes over an existing workspace connection — the same trust tier as connect and disconnect. No new permission; `integrations.write` gating with the existing 403 envelope mapping.

## Risks / Trade-offs

- [Concurrent agent edits clobbered by array rewrites] → tx-scoped read-modify-write touching only the connection's id in each array (D1).
- [Agent deleted between dialog load and save] → server re-validates every id inside the apply; unknown id rejects atomically (spec scenario).
- [Probe latency on replace reads as a hang] → explicit "Verifying…" save state; the dialog disables both sections until the probe resolves.
- [Large workspaces make the agent list long] → the list scrolls inside the dialog; no pagination in v1.
- [Token replace on an `expired` OAuth connection] → unreachable: OAuth kinds have no token field and the endpoint refuses them (spec scenario).
- [Race: connection disconnected while edit dialog open] → both endpoints 404 on the missing connection; the dialog surfaces the error and closes.

## Migration Plan

None — no schema change (attachment lives in agent rows; secret rows exist). Deploy is an ordinary rolling backend+web release; rollback is reverting the release (the two endpoints disappear, existing flows are unchanged).

## Open Questions

None — the three explore-time decisions are locked: OAuth attach lands on callback success (D4); scope is agents + token only; per-agent credentials and access-level edits stay out.
