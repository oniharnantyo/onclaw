# Design — agent-roster-search-sort

## Context

The roster's data is already fully client-side: `loadAgents` merges `GET /workspaces/:ws/agents` into `tenant.agents`, and workspace agent counts are small (dozens). No list endpoint in the server accepts query parameters today — `c.Query()` has zero usages — so server-side search/sort would invent a convention for a single screen. Separately, `ListAgents` currently composes `IDENTITY.md`/`SOUL.md`/`BOOTSTRAP.md` into every roster entry (per-agent disk reads, dead payload weight), while the backend `agents` spec already specifies the list as a summary roster that excludes prompt documents. The sidebar renders agent rows with hardcoded initials (never `a.avatar`), and the config modal edits the store draft without ever fetching the detail endpoint. This change builds on the unarchived `refactor-agent-prompts-to-files` change (implementation already in the working tree). See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- All list interaction (search/sort/pager) computed in the client over the already-loaded roster; no API surface growth.
- Order becomes a pinned, tested contract: `created_at DESC, id DESC` in postgres **and** the fake store.
- List = summary, detail = weight: list responses stop inlining prompt documents, conforming to the `agents` spec, and the one write-path consumer (config modal) is moved to the detail endpoint.
- Sidebar matches the cards: agents render their picked avatar and the newest-first store order everywhere.

**Non-Goals:**
- Server-side list query parameters, pagination, or URL-synced page state.
- Real run-status wiring (the roster's "running/last active" remains client-side defaulting).
- Persistent per-user UI preferences.
- Changing the sidebar ⌘K search scope (name+role stays).

## Decisions

### D1. Client-side list interaction over server-side query parameters
Search, sort, and the pager slice `tenant.agents` at render time (`useMemo`), over the store the roster already loads. Alternatives: server-side `q`/`sort`/`page` params — rejected: zero existing `c.Query()` convention, tenant scale is small, and it would grow the API for one screen; conventions get extracted from a second real consumer, not invented for the first.

### D2. Flip the backend default order instead of re-sorting per surface
`ListForWorkspace` becomes `ORDER BY created_at DESC, id DESC` in postgres, mirrored by the fake store's sort. Every surface inherits newest-first (sidebar, roster default, cron dropdown, mention menus, default chat = `agents[0]`). Alternative: client-side re-sorts per surface — rejected: three sites re-deriving the same ordering eventually disagree; the API order is the one source of truth. This is a **visible behavior change** (default chat becomes the newest agent), accepted in the proposal.

### D3. Order assertions in both store test suites
No test pins list order today (`agents_test.go` asserts only membership). Create A→B→C, expect C,B,A in `agents_test.go` and `fake_test.go`. Alternative: pin only in postgres tests — rejected: the fake mirrors SQL ordering for every handler/roster test that runs against it; an unpinned fake silently diverges from the SQL contract.

### D4. List = summary (trim conforms to the `agents` spec)
Delete the `composePromptDocuments` loop from `ListAgents` — fields serialize as zero values; `GetAgent` (and create-refresh, patch, regenerate responses) keep composing. This removes the N-agents × 3-file-reads cost per roster load. Alternative: keep composing on list — rejected: payload weight with no consumer, contradicts the existing `agents` spec.

### D5. Modal fetch-on-open is a coupled requirement, not an enhancement
The config modal initializes `brief/identity/soul` from the store draft and never fetches. Once the list omits prompt documents, editing would open with empty identity/soul textareas, and saving would write empty strings to the prompt files — **a data-wiping regression**. Therefore D4 is only safe with the modal fetching `GET /workspaces/:ws/agents/:id` on open (edit mode) and hydrating from the detail response. Alternative: keep modal on store draft — rejected: latent staleness bug becomes prompt loss.

### D6. Pager on top of the filter pipeline, with derived clamping
Pipeline: `filter(search) → sort → slice(page)`; footer hidden at ≤24 matches; page resets to 1 on search/sort change; the rendered page is derived (`Math.min(page, pageCount)`), never stored post-hoc — so deleting the last agent on the last page can never show an empty page. Page size 24 divides evenly into the 3/2/1-column contract breakpoints (8/12/24 rows). Alternative: URL-synced page state — rejected: nobody deep-links to roster page 3; adds state synchronization for no consumer.

### D7. Ephemeral toolbar state
Search/sort/page live in `useState` in `AgentsView`; nothing persists across navigation, matching the sidebar ⌘K behavior. Alternative: localStorage persistence — rejected: a persistent "my sort" is a settings feature wearing a list feature's clothes.

### D8. Native styled `<select>` for sort
Three options don't justify a custom listbox; native select gets contract styling (`h-8 border-line text-[13px]`, chevdown icon overlay) and free keyboard/a11y behavior. Alternative: reuse `TimezoneSelect`-style custom listbox — rejected: heavy aria plumbing for a 3-item choice.

### D9. Sidebar avatar is a one-prop fix; scope divergence is documented, not unified
`Sidebar.tsx` passes `avatar={a.avatar}` to `Avatar`. ⌘K keeps name+role matching (navigation finds agents by what they do; roster narrows by identity) — the difference is now specified in `web-app/shell` rather than accidental. Alternative: unify both searches on name-only — rejected: narrowing navigation search would hide agents by role from a surface whose job is discovery.

### D10. Sequencing with the unarchived prompts change
Implement on top of the prompts change's working-tree code; **archive order: prompts change first, this change second**. The deltas touch different requirements (`Agent fields` vs `Agent CRUD`), so archive-time merges stay clean. Task 5.2 of the prompts change ("read-side composition: list/get/...") is partially superseded — this change removes list composition.

## Risks / Trade-offs

- [Default chat becomes the newest agent on workspace switch] → Accepted and documented in the proposal; arguably an improvement (most recent work surfaces first).
- [Cron editor and @mention menus flip to newest-first] → Accepted consequence of D2; consistent surfaces beat a frozen sidebar.
- [Editing an agent right after the trim could wipe prompts] → D5 makes detail-fetch a hard prerequisite; modal test asserts identity/soul populated on open.
- [Store tests never pinned order, so a future refactor could flip it silently] → D3 adds order assertions in both suites.
- [Two search scopes in one app (roster name-only, sidebar name+role)] → Documented in `web-app/shell` delta as intentional; revisit only with user demand.
- [Implementing against a dirty tree mid-prompts-refactor] → D10 pins archive order; code overlap is limited to `ListAgents`, where the trim supersedes.
