## 1. Schema & Store

- [x] 1.1 Migration `000041_agent_sessions` (up/down pair per design D1: table, unique triple, listing index); verify with `migrate up` + `migrate status` on dev DB
- [x] 1.2 Domain type `domain.AgentSession` (id, workspace/agent/user ids, session id, title, deleted_at, created_at, last_active_at) + `store.AgentSessionStore` sub-interface: `Upsert`, `ListByAgent(workspaceID, agentID, userID)` (non-deleted, newest activity first), `SoftDelete(workspaceID, agentID, sessionID, userID)`
- [x] 1.3 Postgres adapter for `AgentSessionStore` (upsert with title `CASE` guard + `deleted_at = NULL` revive per D2) + fake store implementation; store-level tests: birth sets title, second turn bumps activity only, soft-deleted row excluded from list then revived by rechat, listing is user-scoped
- [x] 1.4 Wire `AgentSessionStore` through the composition root (`internal/cli` → `router.go`) as a positional dependency — no nil guards, no aggregate store

## 2. Runner Indexing

- [x] 2.1 Runner upserts the index at persistent run start (D2): title from first line of `ExecRequest.Input` trimmed ≤42 chars + ellipsis, empty input leaves title unset; ephemeral runs skip; upsert failure logs a warning and does not fail the run
- [x] 2.2 Runner tests: birth turn inserts with derived title; subsequent turn updates `last_active_at` only; compact turn never titles; ephemeral run writes nothing

## 3. Run-Manager Enumeration & API

- [x] 3.1 `runManager.ActiveRunSessionIDs(workspaceID, agentID) []string` — collect live session ids under the manager mutex; unit test with a started and a finished run
- [x] 3.2 `GET /api/workspaces/:slug/agents/:agent/sessions` handler (agents:read): user-scoped rows + `running` flag from the intersection with 3.1; empty list returns `[]` not null
- [x] 3.3 `DELETE /api/workspaces/:slug/agents/:agent/sessions/:session` handler (agents:write): soft-delete own row; foreign-owned or absent → 404; register both routes in `router.go`
- [x] 3.4 Handler tests: listing scoped to requesting user, running flag set while a run is live, soft delete hides from listing, 404 on foreign row; `scripts/smoke.sh` coverage for both endpoints (create session via turn → list shows it with title → delete → list hides it)

## 4. Web Session List

- [x] 4.1 API client: `listAgentSessions(slug, agentSlug)` + `deleteAgentSession(...)` in `web/src/lib/api.ts` with types
- [x] 4.2 Store reconciliation (D4): on agent chat open, render from local overlay then fetch and reconcile — server rows authoritative for index/order/titles/running, pre-index local sessions appended below; TS test for reconcile ordering and local-append rule
- [x] 4.3 Refetch triggers: own turn terminal event, window focus/visibilitychange, sidebar expand, cross-tab `storage` event on `onclaw.threads.v1`; single debounced refetch coalescing; test the trigger wiring
- [x] 4.4 Delete flow: call endpoint first, then local removal; active-deleted session falls back to an adjacent session or fresh "New chat" (existing last-session rule); test both paths
- [x] 4.5 Optimistic title parity: keep client-side titling, assert in a test that client and server rules agree on the same fixtures (multiline input, long input, empty input)

## 5. Running Indicator

- [x] 5.1 Sidebar session row dot (mockup A): hollow idle dot, filled pulsing accent dot while running (150/200ms motion contract); state = local run state OR server `running` flag, exactly one pulse per session
- [x] 5.2 Wire local source: existing run start/terminal state flips the dot instantly on send and back at terminal; test both flips
- [x] 5.3 Wire server source: reconciled list marks foreign-run sessions pulsing; refreshed list without the flag stops the pulse; test with a mocked list response

## 6. Verification

- [x] 6.1 `go build ./... && go vet ./... && go test ./...` green; integration tests against dev DB green
- [x] 6.2 `pnpm --filter web test` touched suites green (per repo convention: judge by touched suites, not full-parallel red counts) and production build green
- [x] 6.3 Full `./scripts/smoke.sh` pass
- [x] 6.4 Manual browser pass (2026-09-11, live on dev): server-backed list renders under the agent with titles and last-active order (fresh-reload approximates incognito — auth is localStorage-shared); rechat: message sent in the oldest session bumped it to top of sidebar and DB `last_active_at` reordered; cross-tab: run started in a second tab, first tab refreshed → running row pulsed (`animate-pulse` + leading spinner + "run in progress" tooltip); delete: row vanished immediately, stayed gone after reload, DB `deleted_at` set (soft delete); direct agent URL did not resurrect it
