# Tasks — agent-roster-search-sort

> Sequencing note (design D10): implement on top of the `refactor-agent-prompts-to-files` working-tree code; archive that change **before** this one.

## 1. Store: newest-first ordering

- [x] 1.1 Confirm prompts-refactor code is present in the tree (`composePromptDocuments` in `internal/server/handlers/agents.go`, `internal/agents/workspace.go`) — this change builds on it
- [x] 1.2 Flip `ListForWorkspace` ordering in `internal/store/postgres/agents.go` to `ORDER BY created_at DESC, id DESC`
- [x] 1.3 Mirror the ordering in the fake store (`internal/store/fake/fake.go`, `agentStore.ListForWorkspace`): sort by `CreatedAt` descending, tiebreak `ID` descending
- [x] 1.4 Add order assertions to `internal/store/postgres/agents_test.go`: create agents A→B→C, expect list C,B,A
- [x] 1.5 Add the same order assertion to `internal/store/fake/fake_test.go`

## 2. Handlers: summary roster

- [x] 2.1 Remove the `composePromptDocuments` loop from `ListAgents` in `internal/server/handlers/agents.go` (get/create-refresh/patch/regenerate responses keep composing)
- [x] 2.2 Add handler test: list entries omit identity/soul/bootstrap document content; the detail response includes them

## 3. Roster toolbar (AgentsView)

- [x] 3.1 Search input: name-only, case-insensitive substring match; sidebar-idiom styling (search icon at `left-2.5`, `h-8 border-line rounded-md pl-8 text-[13px] focus:border-accent`); `data-testid="agents-search"`
- [x] 3.2 Sort select: native `<select>` with contract styling and chevdown overlay; options Newest first (default) / Name A–Z / Oldest; `data-testid="agents-sort"`; `useMemo` pipeline `filter → sort` over `tenant.agents`
- [x] 3.3 Filtered count: sub line reads "N of M agents in <workspace> …" while a query is active; full count otherwise
- [x] 3.4 No-match state: "No agents match \"<q>\"" with a Clear search action resetting the input
- [x] 3.5 Tests (`AgentsView.test.tsx`): name filtering (and role-word non-match), sort switching (newest default / name / oldest), no-match + clear restores roster

## 4. Pager

- [x] 4.1 Slice + footer: 24 cards/page; footer `‹ Prev · Page X of N · Next ›` with Prev/Next disabled at bounds, hidden entirely at ≤24 matches; `<nav aria-label="Pagination">`; `data-testid="agents-pager"`
- [x] 4.2 Page resets to 1 on search/sort change; rendered page derived as `Math.min(page, pageCount)` (clamp, no stored correction)
- [x] 4.3 Tests: slicing shows cards 25–48 on page 2, bound-disabled states, footer hidden at ≤24, reset-on-search, clamp-when-shrinking

## 5. Modal detail hydration

- [x] 5.1 `AgentConfigModal`: on open in edit mode, fetch `api.agents.get(wsId, agentId)` and hydrate the form (including brief/identity/soul/bootstrap) from the detail response, with a loading state while in flight; do not seed from the store draft
- [x] 5.2 Modal test: opening edit populates identity/soul from the detail fetch; saving without edits leaves prompt documents unchanged (no empty-string PATCH)
- [x] 5.3 Confirm no other client consumer of list-carried prompt documents exists (chat runtime must not read identity/soul from the store)

## 6. Sidebar avatar

- [x] 6.1 `web/src/components/nav/Sidebar.tsx`: pass `avatar={a.avatar}` to the agent-row `Avatar` (initials fallback preserved for empty configs)
- [x] 6.2 Audit remaining `Avatar` usages (chat header, @mention menu): pass the agent's avatar config where missing; report the audit result

## 7. Verification

- [x] 7.1 `rtk go build ./... && rtk go vet ./... && rtk go test ./...`
- [x] 7.2 Postgres integration tests with order assertions: `go test -tags=integration ./internal/store/postgres/` (requires `TEST_DATABASE_URL`)
- [x] 7.3 Frontend tests: `cd web && rtk proxy npx vitest run` (project memory: the rtk vitest wrapper is flaky with async RTL tests — use `rtk proxy`)
- [x] 7.4 `cd web && rtk pnpm build` (typecheck + production build)
- [x] 7.5 `./scripts/smoke.sh` — roster order and list payload remain compatible end-to-end
