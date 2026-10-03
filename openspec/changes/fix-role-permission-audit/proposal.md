# Proposal

## Why

A 2026-10-02 permission audit against the target model (superadmin = anything incl. create workspaces; admin = manage anything in a workspace; member = read-only + basic agent use) found enforcement gaps on both sides: Members can modify shared surfaces the model reserves for admins (reference documents, workspace creation), Members lack basic-chat affordances the model grants them (approve own tool calls, cancel own runs, delete own threads, channels), and the web shows admin-only controls as clickable for Members. Enforcement itself is a flat slice scan in one middleware; the audit is the moment to move it onto a rules engine and land the decided grants as policy data.

## What Changes

- **Switch permission enforcement to Casbin (engine-only shape).** New `internal/authz` port; a Casbin enforcer (RBAC-with-domains model) is the single evaluation point behind `RequirePermission`/`RequireAllPermissions` and the in-handler checks (`members.go`, `agents.go` connection-approval escalation, `skill_curation.go`, `internal/agents/connection_gate.go`). Policies sync from the existing `roles` table (still the source of truth) at boot and at the workspace-creation seeding seam. No `casbin_rule` table, no storage adapter. Behavior-preserving, proven by golden parity tests over every built-in role × every catalog permission.
- **BREAKING: Workspace creation becomes superadmin-only.** `POST /workspaces` requires master-workspace membership with `admin.workspaces.write`; the web switcher's "+ new" affordance and the onboarding create flow render only for instance admins, with an "ask your admin" state for everyone else.
- **BREAKING: `roles.write` is removed from the permission catalog.** No endpoint ever enforced it (role CRUD does not exist); it is dropped from the closed catalog, the built-in role sets, and existing role rows via migration.
- **Member own-session grants.** Cancelling a run, resolving a tool approval, and deleting a session are permitted for the session's owning member (new ownership rule), otherwise still require `agents.write`. Connection-write approvals keep their `integrations.write` escalation regardless of ownership. `/v1` `metadata.onclaw_session` binding becomes owner-scoped: a key may bind only sessions created by the key's creating user — **BREAKING** for cross-user session continuation, which now resolves not-found.
- **Member channels grant.** `channels.read` and `channels.write` join the built-in Member permission set (and are backfilled into existing workspaces' Member rows), so members can list channels, post messages, and manage channel membership — matching the channel specs, which already assume member posting.
- **Reference documents: own-docs-only.** Members may upload and may edit/delete/replace only documents they uploaded; mutating another member's document requires `workspace.write`; attaching a document to agents/channels requires the corresponding `agents.write`/`channels.write`; promote/demote stay `reference_documents.promote`. The Documents-pane listing lens for members is scoped per the existing "own scope only" requirement.
- **API-key creator symmetry.** The creating member can list and revoke their own exchanged keys; listing/revoke of others' keys still require `workspace.write`. Key minting stays member-level (the chat lane depends on it).
- **Web gating sweep.** Members-pane invite/role/remove, providers add/delete, tools-pane toggles, scheduler mutations, agent deploy/save, workspace save, and channel-member management hide or disable for Members using the existing `canWrite*` helper family; HooksPane gates on `hooks.write` (not `tools.write`) and the memory surfaces gate on `workspace.write`, matching the backend catalog.

## Capabilities

### New Capabilities

(none — authorization mechanics are implementation; every behavior delta lands in an existing capability)

### Modified Capabilities

- `members-roles`: permission catalog redefined (Member gains channels.read/write; roles.write removed); built-in Member role description updated; member-management guard scenarios unchanged.
- `tenancy`: workspace creation restricted to superadmin (master-workspace member holding admin.workspaces.write); birth transaction semantics unchanged.
- `workspace-reference-documents`: ownership-scoped mutations and attachment gating (upload stays member-level; edit/delete/replace own-docs; others' docs need workspace.write; attach needs agents.write/channels.write; listing lens enforced).
- `openresponses`: session binding owner-scoped; API-key lifecycle gains creator-scoped list/revoke.
- `agent-runtime`: cancellation and approval resolution extended to the session's owning member; connection-write approvals keep integrations.write.
- `web-app/workspaces`: creation flow + onboarding reworked for superadmin-only creation.
- `web-app/settings`: Members/Providers/Tools/API-keys/Workspace panes gated per role; Hooks and memory panes re-gated to the correct permissions.
- `web-app/chat`: approval cards, cancel, and session delete actionable for the owning member; connection-approvals stay read-only for members.

## Impact

- **Backend:** `internal/domain/permissions.go` (catalog, built-in sets), new `internal/authz` package (port + Casbin implementation + fake), `internal/server/middleware.go` + `router.go` (guard wiring), handlers `members.go` / `agents.go` / `skill_curation.go` / `reference_documents.go` / `api_keys.go` / `workspaces.go`, `internal/server/handlers/v1.go` (session binding), `internal/agents/connection_gate.go`, bootstrap/role seeding, new migrations (Member-role channels backfill; `roles.write` strip).
- **Frontend:** `web/src/store/auth.ts` helper reuse, `MembersSection`, `ProvidersPane`, `ToolsPane`, `SchedulesView`/`ScheduleEditorModal`, `AgentsView`/`AgentConfigModal`, `WorkspaceSection`, `KeysSection`/`KeyRow`, `HooksPane`, `MemoryPane`, `WorkspaceSwitcher`, `CreateWorkspaceModal`, `OnboardingPane`, sidebar channels section, `AgentMessage` approval cards, session delete/cancel affordances.
- **Dependencies:** adds `github.com/casbin/casbin/v2` (no storage adapter).
- **Data:** one migration backfills `channels.read`/`channels.write` into built-in Member rows and strips `roles.write` from all role rows; no other schema change (no `casbin_rule` table).
