# Design

See proposal.md for motivation and scope.

## Context

Permission enforcement lives in exactly one middleware pair (`RequireWorkspace` → `RequirePermission`, `internal/server/middleware.go`) plus four in-handler consumers (`handlers/members.go`, `handlers/agents.go`, `handlers/skill_curation.go`, `internal/agents/connection_gate.go`). The check itself is `domain.HasPermission` — a linear scan of the role's `permissions text[]` loaded per request. Role→permission rows are created at workspace seeding and are **immutable at runtime** (no role-CRUD endpoints exist). The web derives gating booleans from the `auth/me` role payload via a `canWrite*` helper family. The decided behavior changes (proposal) touch eight existing capabilities; none of them require new spec surfaces.

## Goals / Non-Goals

Goals:
- One evaluation point for "does role R in workspace W hold permission P", backed by a rules engine.
- The four decided behavior changes land as data (policy lines, permission sets) plus narrow ownership checks.
- Zero behavior drift during the mechanism swap (golden parity).

Non-Goals:
- No `casbin_rule` table, no storage adapter, no policy admin UI (engine-only shape, user-decided).
- No custom-role CRUD (`roles.write` is removed, not wired).
- No change to `RequireWorkspace` membership semantics, the strict-subset member-management algebra, or the master-tenant control plane.
- No superadmin bypass of workspace membership (two-step self-add via admin console stays).

## Decisions

### D1 — Authorizer port, Casbin behind it (`internal/authz`)
`internal/authz` exposes a narrow interface (`Enforce(ctx, roleID, workspaceID, permission) (bool, error)` plus a `Sync(ctx, role)` write-through used by seeding). The Casbin implementation owns an embedded model and an in-memory policy set; the fake implementation mirrors it for unit tests. Composition root (`internal/cli`) builds the enforcer, loads all role rows once, and injects the port into the middlewares and handlers as a positional parameter. **Why:** the repo's DI and plugin-first rules forbid a hard Casbin coupling in `server`; the port keeps the engine swappable and gives `connection_gate.go` (non-HTTP consumer) the same seam. **Alternative rejected:** casbin_rule persistence via the pgx adapter (dual source of truth, migration surgery on the roles API — deferred until role CRUD is wanted; the port hides that future).

### D2 — Policy model: role-as-subject, no grouping lines
Model is RBAC-with-domains without `g` lines: `p, roleID, workspaceID, permission` with a matcher allowing exact and `keyMatch` permission patterns. The middleware already resolves the concrete role per request, so user→role grouping would only duplicate the members table. Policy lines are synced from `roles.permissions` at boot and at workspace-creation seeding (the only runtime write seam; `Sync` on role create keeps future custom roles one hook away). **Why:** smallest blast radius; member add/remove/role-change flows stay untouched.

### D3 — Session ownership rule
Sessions already carry a per-user attribution (the per-user session-index requirement). Define **session owner** = that user; system-born sessions (channel, scheduler, heartbeat, subagent) have no owner. The three routes (cancel run, resolve approval, delete session) permit a workspace member when `session.user == actor` **or** the actor holds `agents.write`; ResolveApproval's in-handler `integrations.write` escalation for connection-tool approvals applies regardless of ownership (credentials are never member-approvable). `/v1` `metadata.onclaw_session` binding resolves only sessions owned by the key's creating user; binding a system session or another user's session resolves not-found, indistinguishable from foreign-workspace sessions. **Trade-off:** API continuation of channel/system agent sessions is no longer possible from user keys — no spec or surface uses it today; a future explicit grant can reopen it.

### D4 — Workspace creation gate
The public `POST /workspaces` route gets `RequireMasterWorkspace` + `RequirePermission(AdminWorkspacesWrite)` chained ahead of the existing handler; the handler's birth transaction (built-ins, creator-as-Owner, atomic provider/starter-agent) is unchanged. The admin console's `/admin/workspaces` lane keeps working unchanged. Web: the switcher's "+ new" and `CreateWorkspaceModal` render only for instance admins (`useIsAdmin`); a member with zero workspaces sees an "ask your admin" empty state instead of the onboarding create flow.

### D5 — Catalog surgery
`roles.write` leaves the closed catalog, `IsValidPermission`, all built-in sets, and — via migration — every existing role row that holds it. Member gains `channels.read` + `channels.write` in the domain sets and via the same migration (backfill into existing workspaces' Member rows). The channels.write over-grant (channel CRUD + membership management by members) is an **accepted consequence** of the user's "grant read + post" decision; if it proves too broad, a narrower `channels.post` can be split out later as its own change.

### D6 — Reference-document mutation matrix
Upload stays member-level. Patch/PutContent/Delete/replace: uploader or `workspace.write`. PutAgents: `agents.write`. PutChannels: `channels.write`. Promote/demote: `reference_documents.promote` (unchanged). The member listing lens follows the existing spec sentence — own uploads plus documents attached to agents/channels the viewer can configure; promote-holders see all. Checks live in `handlers/reference_documents.go`; no new permission is introduced.

### D7 — API-key creator symmetry
List returns the caller's own keys for members, all keys for `workspace.write` holders. Revoke: key creator or `workspace.write`. Exchange (member-level) is untouched — the chat lane depends on it. This removes the audit's asymmetry (member mints keys it could never see or revoke) without moving key management to members at large.

### D8 — Web gating mechanics
Reuse the existing `canWrite*` helpers; no new permission framework. Ungated surfaces get gated (Members pane controls, Providers add/delete, Tools toggles, scheduler mutations, agent deploy/save, Workspace save, API-keys create/revoke within D7's rules, channel-member add/remove stays — now legitimate for members). Permission corrections: HooksPane and the agent-config hooks section gate on `hooks.write`; MemoryPane promote/delete/consolidate gate on `workspace.write`. Chat: ordinary approval cards, cancel, and session delete become actionable when the viewer owns the session (client derives from the session's owner field); connection-approval cards keep the existing `integrations.write` read-only treatment.

## Risks / Trade-offs

- [Casbin is a new dependency and idiom for the codebase] → the port (D1) confines it to one package; golden parity tests pin behavior to the current catalog before any grant changes.
- [Policy sync drift if a future role-write seam forgets `Sync`] → role rows are immutable at runtime today; `Sync` is called at the only existing seam (seeding) and documented as mandatory for future role CRUD.
- [channels.write over-grant lets members create/delete channels and manage channel membership] → user-accepted (D5); narrowest-review follow-up if it proves too broad.
- [/v1 owner-scoped binding breaks API continuation of other users' or system sessions] → intentional (D3); error is the standard not-found envelope.
- [Member-role backfill touches every existing workspace] → single idempotent migration (add two perms, strip one), verified by the integration suite; no data besides permission arrays changes.

## Migration Plan

1. Deploy order is single-binary: one migration adds `channels.read`/`channels.write` to built-in Member rows and strips `roles.write` from all rows; the binary boots the enforcer from the post-migration rows.
2. Rollback: revert binary; the migrated permission arrays remain valid under the old catalog check (unknown strings are inert to `HasPermission`; `roles.write` loss on Owner/Admin rows is unobservable — no endpoint ever read it).
3. The fake store's role fixtures gain the same set changes so fake-based and integration tests exercise identical sets.

## Open Questions

None — the four product decisions, the store shape, and the sequencing were decided with the user before capture.
