## Why

The agents screen renders every agent in a fixed backend order with no search, no sort control, and no pager, so finding a specific agent means scanning cards visually; a workspace with dozens of agents has no fast path to one. Meanwhile agents' picked avatars render on roster cards but not in the sidebar, so the same agent has two faces depending on surface. The backend already returns the full roster to the client, making client-side list interaction the cheapest correct layer for all of it.

## What Changes

- Roster toolbar: a name-only search input and a sort select (Newest first / Name A–Z / Oldest), defaulting to Newest first; while a search is active the header sub line shows "N of M agents …".
- Numbered pager under the grid: 24 cards per page (divides evenly into the 3/2/1-column breakpoints), Prev/Next with "Page X of N", hidden entirely when results fit one page; page resets to 1 on search/sort change and clamps when the result set shrinks.
- List responses stop inlining per-agent prompt documents: `ListAgents` no longer reads/composes `IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` into every roster entry (conforming to the already-specified summary roster; also drops N×3 file reads per roster load).
- The agent config modal, opened to edit, hydrates from the detail endpoint (`GET /workspaces/:ws/agents/:id`) instead of the store draft, so edits always start from fresh data (required once list responses omit prompt documents, otherwise editing could wipe them).
- Sidebar agent rows render the agent's picked avatar (falling back to initials) instead of always initials, and inherit the newest-first store order.
- Backend list ordering flips to `created_at DESC, id DESC` (newest-first) in both postgres and fake stores, pinning order as a tested contract; switching workspaces now opens the newest agent's chat (first element of the roster).
- Sidebar ⌘K search intentionally keeps its name+role scope (roster search is name-only); the two behaviors are now documented rather than accidentally different.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/agents`: the Agents roster requirement gains search, sort, filtered count, pager, and avatar display language; the structured configuration requirement gains a hydrate-from-detail scenario for edit mode.
- `web-app/shell`: navigation gains agent-row avatar display and newest-first agent ordering in the sidebar; ⌘K search scope (name+role) documented as intentional.
- `agents`: Agent CRUD requirement pins list ordering (created newest-first, id tiebreak) and that list responses exclude prompt documents.

## Impact

- **Backend:** `internal/store/postgres/agents.go` (`ListForWorkspace` ORDER BY), `internal/store/fake/fake.go` (mirror sort), `internal/server/handlers/agents.go` (`ListAgents` drops the composePromptDocuments loop). No migrations, no route changes.
- **Frontend:** `web/src/screens/AgentsView.tsx` (+ `AgentsView.test.tsx`), `web/src/components/nav/Sidebar.tsx`, `web/src/modals/AgentConfigModal.tsx`, `web/src/store/index.ts` (merge no longer expects composed prompt docs from list).
- **Tests:** new order assertions in `internal/store/postgres/agents_test.go` and `internal/store/fake/fake_test.go`; handler test asserting list omits prompt documents while detail includes them; roster UI tests (filter, sort, pager, no-match state); modal hydrate-from-detail test.
- **Coordination:** builds on the unarchived `refactor-agent-prompts-to-files` change (whose implementation is already in the working tree). Implement on top of it; archive this change only after that one. This change supersedes the list-composition part of that change's task 5.2; its spec deltas touch different requirements ("Agent fields" vs "Agent CRUD"), so no delta merge conflict is expected.
- **Known follow-on effects (accepted):** switching workspaces opens the newest agent's chat (`agents[0]`); cron editor and `@mention` menus list agents newest-first.
