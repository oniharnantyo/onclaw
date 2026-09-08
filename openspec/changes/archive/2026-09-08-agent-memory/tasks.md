## 1. Migration & schema

- [x] 1.1 Write `migrations/000025_agent_memory` up/down: create `user_memories` (PK workspace_id,user_id), `agent_daily_memories` (PK workspace_id,agent_id,memory_date), add `workspaces.memory text NOT NULL DEFAULT ''`, add `UNIQUE (workspace_id, id)` on agents + composite FK from agent_daily_memories, drop `agent_user_memories`; down reverses (recreates old table empty)
- [x] 1.2 Verify migration applies to the dev database and reverses cleanly (`migrate up` / `migrate status`)

## 2. Store layer

- [x] 2.1 Add `domain.Memory` content validator: shared char-cap check (`maxMemoryContentChars = 32000`) with cap-exceeded error naming current size, cap, and the human-trim escape hatch
- [x] 2.2 Add `store.MemoryStore` sub-interface (UserMemory/WorkspaceMemory/AgentDailyMemory Get/Upsert) to `store.Store`
- [x] 2.3 Implement the fake store methods with workspace-scoped maps
- [x] 2.4 Implement the postgres adapter: point reads, upserts on the composite PK conflict targets, workspace scope as targeted `UPDATE workspaces SET memory` (never via WorkspaceStore.Update)
- [x] 2.5 Implement atomic append: single upsert statement with `content = content || $n WHERE length(content) + length($n) <= cap` guard
- [x] 2.6 Store tests: fake + postgres integration (scope isolation, daily upsert-per-day, append atomicity, cap rejection)

## 3. Memory tool

- [x] 3.1 Extend `ToolContext` with `UserID` and `WorkspaceTZ`; plumb from runner's loaded user/workspace in `resolve()`
- [x] 3.2 Implement the `memory` tool in `internal/agents/tools`: path grammar (exact USER.md/WORKSPACE.md, strict dd-mm-yyyy parse, MEMORY-TODAY resolved in workspace TZ), read/append actions, JSON error results, append confirmations naming the resolved date document
- [x] 3.3 Register in `NewDefaultToolRegistry` (binds MemoryStore + identity from ToolContext) and add the non-configurable catalog entry (`memory`, group `memory`, icon)
- [x] 3.4 Tool tests: path parsing (accepted/rejected forms incl. format-hint error), empty-read marker, append-without-content error, cap-exceeded tool error, confirmation wording

## 4. Instruction injection

- [x] 4.1 Extend the composer: `## Shared memory` subsection under `# Workspace` and `## Memory` under `# User`, loaded via MemoryStore per execution; omitted when empty
- [x] 4.2 Composer tests: subsections present with content, absent when empty, per-execution freshness (second turn sees first turn's append)

## 5. Birth ritual & prompts

- [x] 5.1 Add jail-scoped `delete_file` to the fs toolset (os.Remove inside the jail; missing file → tool error; path escape rejected) + tests
- [x] 5.2 Update embedded `promptdocs/AGENTS.md` with the Memory section (read/append, three scopes, never re-store free context, keep entries short)
- [x] 5.3 Update embedded `promptdocs/BOOTSTRAP.md` closing line: complete the beats, then delete BOOTSTRAP.md via delete_file
- [x] 5.4 Delete the old `GET/DELETE /workspaces/:ws/agents/:agent/memory` routes and handlers

## 6. HTTP endpoints

- [x] 6.1 `GET/PUT /api/v1/workspaces/:ws/me/memory` — membership-only, self-scoped, payloads `{content}` / `{content, max_chars, updated_at}`, 422 over cap
- [x] 6.2 `GET /api/v1/workspaces/:ws/memory` (members) and `PUT` (settings-management permission; targeted update, no other workspace fields touched)
- [x] 6.3 Handler tests: role matrix (member read/write own, member 403 on workspace PUT, admin OK), cap 422, unknown workspace 404

## 7. Frontend

- [x] 7.1 API client: user-memory and workspace-memory get/update calls typed against `{content, max_chars, updated_at}`
- [x] 7.2 UserMenu: "My Memory" entry opening a modal editor (textarea, live counter from max_chars, inline 422 handling, save confirm)
- [x] 7.3 Settings Workspace pane: shared-memory editor below workspace fields — read-only for Members, savable for Owner/Admin, independent of the workspace-details save
- [x] 7.4 Component tests: modal save flow, over-cap inline error, member read-only state

## 8. Verification

- [x] 8.1 `go build ./...`, `go vet ./...`, `go test ./...` green; integration tests with TEST_DATABASE_URL
- [x] 8.2 Extend smoke.sh: seed user memory via PUT, workspace memory via PUT (admin), confirm 403 for member PUT, confirm old agent-memory routes are gone
- [ ] 8.3 Manual browser pass: user-menu editor round-trip, workspace editor round-trip, agent with memory tool appends to MEMORY-TODAY.md and the tool card appears in the transcript, next turn's answer reflects the memory, birth ritual deletes BOOTSTRAP.md on a fresh agent
