# Tasks

## 1. Casbin authorizer port (design D1, D2 — behavior-preserving)

- [x] 1.1 Add `github.com/casbin/casbin/v2` to go.mod; create `internal/authz` with the `Authorizer` port (`Enforce(ctx, roleID, workspaceID, permission) (bool, error)`, `Sync(ctx, *domain.Role) error`, `Reload(ctx) error`), an embedded RBAC-with-domains model (role-as-subject p-lines, `keyMatch` on permission, no grouping lines), and the Casbin implementation built from a role store. Verify: `go build ./...` and a unit test that syncing the four built-in permission sets then enforcing every catalog permission × every role matches `domain.HasPermission`.
- [x] 1.2 Add `internal/authz` fake backed by the same in-memory model for fake-store tests. Verify: fake passes the same parity unit test as the Casbin implementation.
- [x] 1.3 Wire composition (`internal/cli` → `internal/server/router.go`): build the authorizer from the roles store at boot, sync every role row, inject the port into `middlewares` and the handlers that need it as positional parameters. Verify: `go vet ./...`; server boots and `RequirePermission` routes behave identically (existing middleware tests green unchanged).
- [x] 1.4 Swap `RequirePermission`/`RequireAllPermissions` to the authorizer port. Verify: full `go test ./...` green with zero test edits (proves parity); add one middleware test asserting a synced role denies an unsynced permission.
- [x] 1.5 Route the in-handler consumers through the port: `handlers/members.go` (`MembersRemove` check feeding CanEdit), `handlers/agents.go` (connection-approval `integrations.write` escalation), `handlers/skill_curation.go`, `internal/agents/connection_gate.go`. Verify: `go test ./...` and integration tests green.
- [x] 1.6 Call `Sync` at the workspace-creation seeding seam so a newborn workspace's roles enforce immediately without reboot. Verify: integration test — create workspace via API, immediately PATCH an agent with an admin member 200, and a member 403.

## 2. Catalog surgery + migration (design D5)

- [x] 2.1 Domain: remove `RolesWrite` from the closed catalog, `IsValidPermission`, `AllPermissions`, and the Owner/Admin/Superadmin sets; add `ChannelsRead`/`ChannelsWrite` to `MemberPermissions`. Verify: `go test ./internal/domain/...` with updated catalog tests; grep shows no non-test `roles.write` references.
- [x] 2.2 Migration: backfill `channels.read`,`channels.write` into every built-in Member role row and strip `roles.write` from all role rows (idempotent, up/down pairs). Verify: `go test -tags=integration ./internal/store/postgres/...` — pre-migration rows converge to the expected sets, re-running is a no-op.
- [x] 2.3 Update fake-store role fixtures to the new built-in sets. Verify: `go test ./...` green.
- [x] 2.4 Golden parity re-check: after catalog changes, re-run the 1.1 parity test against the new sets. Verify: test green; `./scripts/smoke.sh` full suite passes.

## 3. Member grants: channels + own-session (design D3, D5)

- [x] 3.1 Session ownership fact: expose the session's creating user to the cancel/approval/delete handlers and to `/v1` `resolveSession` (from the existing session index). Verify: unit test on the resolver — user-owned session returns its owner, system sessions (channel/scheduler/heartbeat) return none.
- [x] 3.2 Route rule change: cancel run, resolve approval, and delete session permit the session's owning member OR `agents.write`; others 403. Connection-tool approvals keep the in-handler `integrations.write` requirement regardless of ownership. Verify: handler tests — owner-member 200, non-owner member 403, admin 200; connection-tool approval by agents.write-only holder 403.
- [x] 3.3 `/v1` owner-scoped binding: `metadata.onclaw_session` and `previous_response_id` resolve only sessions owned by the key's creating user; foreign-user and system sessions fail not-found in the OpenResponses envelope. Verify: v1 handler tests for owned / foreign-user / system / cross-workspace cases.
- [x] 3.4 Channel access: verify member list/post/membership flows end-to-end now that Member holds channels.* — no route changes expected. Verify: integration test — member lists channels, posts a message mentioning an agent member (run fires), adds a channel member.

## 4. Superadmin-only workspace creation + key symmetry (design D4, D7)

- [x] 4.1 Guard `POST /workspaces` with master-workspace membership + `admin.workspaces.write` (403 otherwise); birth transaction unchanged. Verify: handler tests — superadmin 201, workspace member 403, unauthenticated 401; atomic-birth tests still green.
- [x] 4.2 API-key creator symmetry: list returns own keys for members (all for `workspace.write`); revoke allowed for the key's creator or `workspace.write`. Verify: handler tests — member sees own exchanged key and revokes it; member revoking another's key 403.
- [x] 4.3 Reference-document mutation matrix: upload unchanged; patch/content/delete/replace = uploader or `workspace.write`; PutAgents = `agents.write`; PutChannels = `channels.write`; member listing lens = own uploads + attached-to-configurable (promote-holders see all). Verify: handler tests per matrix row; integration test for the lens.

## 5. Web gating (design D8)

- [x] 5.1 Settings gating: Members pane (invite/role/remove hidden without members.write/remove), Providers pane (add/edit/delete/toggle hidden without providers.write), Tools pane (toggle/gear hidden without tools.write), Workspace pane (fields read-only + save hidden without workspace.write), Hooks pane re-gated to `hooks.write`, Memory pane promote/delete/consolidate re-gated to `workspace.write`. Verify: pane tests with Member fixtures asserting absent controls; existing admin fixtures unchanged.
- [x] 5.2 Keys pane creator scoping: member sees own keys only with reveal/copy/revoke; `workspace.write` holders see all. Verify: KeysSection test with member fixture (own key visible, revocable) and admin fixture (all keys).
- [x] 5.3 Workspace creation UI: switcher "+ new" renders only for instance admins (`useIsAdmin`); zero-membership users get the ask-your-admin state; onboarding no longer offers creation to non-admins. Verify: WorkspaceSwitcher + zero-membership state tests; App.test admin-rail cases still green.
- [x] 5.4 Chat affordances: approval cards actionable for the session-owning member (connection-write approvals stay read-only without integrations.write), cancel stop control and session delete work on own sessions. Verify: AgentMessage approval-card tests extended with member-owner fixture; composer stop + sidebar delete tests with member fixture.
- [x] 5.5 Scheduler and agent-save gating: "New schedule"/edit/run-now/delete controls and agent deploy/configure-save buttons hidden or disabled without `scheduler.write`/`agents.write`. Verify: SchedulesView/AgentConfigModal tests with Member fixtures.
- [x] 5.6 Full web suite. Verify: `pnpm test` (or repo equivalent) all green.

## 6. End-to-end verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` and `go test -tags=integration ./...` all green.
- [x] 6.2 `./scripts/smoke.sh` full suite green on a fresh `onclaw_smoke` database (drop + recreate first).
- [ ] 6.3 Live pass (user-gated): member account sees read-only admin surfaces, can chat + approve own tool call + cancel + delete own thread + use channels; superadmin creates a workspace; member cannot.
- [x] 6.4 Update `docs/` permission notes if any exist; confirm openspec validate stays strict-clean.
