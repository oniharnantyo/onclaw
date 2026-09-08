## ADDED Requirements

### Requirement: Three memory scopes
Agent memory SHALL consist of three independently scoped, append-friendly markdown documents: `USER.md` — memory about the current user, one document per (workspace, user) pair; `WORKSPACE.md` — shared team memory, one document per workspace; `MEMORY-DD-MM-YYYY.md` — the agent's private log for one specific day, one document per (workspace, agent, date). All three SHALL be persisted server-side (database-backed, not files in the agent jail), and every read/write SHALL be scoped to the executing run's workspace, agent, and user — no argument can address another user's, agent's, or workspace's memory. Writing a daily memory for a day that already has one SHALL update that day's document in place; dates SHALL be parsed strictly as dd-mm-yyyy, and the reserved name `MEMORY-TODAY.md` SHALL resolve to today's date in the workspace's timezone.

#### Scenario: Daily memory upserts per day
- **WHEN** an agent appends to `MEMORY-TODAY.md` twice on the same day
- **THEN** both appends land in the single document for that workspace/agent/date

#### Scenario: TODAY resolves in workspace timezone
- **WHEN** an agent writes `MEMORY-TODAY.md` in a workspace whose timezone makes the UTC date differ from the local date
- **THEN** the memory is stored under the workspace-local date

#### Scenario: Strict date parsing
- **WHEN** an agent addresses `MEMORY-2026/09/08.md` or any non-dd-mm-yyyy date
- **THEN** the tool returns an error naming the accepted forms and stores nothing

### Requirement: Memory tool
Agents with the `memory` tool allowlisted SHALL have exactly one memory tool exposing the three scopes through a `path` argument (`USER.md`, `WORKSPACE.md`, `MEMORY-DD-MM-YYYY.md`, `MEMORY-TODAY.md`) and an `action` argument limited to **read** and **append**. `append` SHALL atomically add content to the end of the resolved document (no read-modify-write window) and SHALL require content; `read` SHALL return the document's content, or an explicit empty marker when nothing is stored. There SHALL be no overwrite action — agent-side correction is additive. Tool failures SHALL return structured error results that do not interrupt the run. Successful appends SHALL confirm the concrete resolved document (including the actual date for `MEMORY-TODAY.md`).

#### Scenario: Read empty memory
- **WHEN** an agent reads `USER.md` before anything was stored
- **THEN** the tool returns an explicit empty marker rather than an empty string or an error

#### Scenario: Append without content fails softly
- **WHEN** an agent invokes `append` with no content
- **THEN** the tool returns a structured error result and the run continues

#### Scenario: Append confirmation names the date
- **WHEN** an agent appends to `MEMORY-TODAY.md`
- **THEN** the result names the resolved concrete date document

### Requirement: Memory size cap
All three memory scopes SHALL share a server-defined size cap expressed as a character budget approximating a token budget. The cap SHALL be enforced identically on every write path — the memory tool (as a structured tool error naming the current size, the cap, and the human-trim escape hatch), the memory HTTP endpoints (422), and surfaced to UI editors for a live counter. Appends SHALL be checked against the resulting document size, not just the appended fragment.

#### Scenario: Tool append over cap
- **WHEN** an agent appends content that would push a memory past the cap
- **THEN** the tool returns a structured error, the stored memory is unchanged, and the run continues

#### Scenario: HTTP save over cap
- **WHEN** a user PUTs memory content past the cap via an edit endpoint
- **THEN** the response is 422 naming the limit

### Requirement: User memory endpoint
Any workspace member SHALL be able to view and edit **their own** `USER.md` via `GET/PUT /api/v1/workspaces/:ws/me/memory`. Access SHALL require membership only — no separate permission. PUT accepts `{content}` and persists it; PUT with content past the size cap SHALL be 422. There SHALL be no way for a member to read or write another member's user memory.

#### Scenario: Member edits own memory
- **WHEN** a member PUTs new content to their own memory endpoint
- **THEN** the content persists and subsequent GET returns it

#### Scenario: Scoped to the caller
- **WHEN** two members of the same workspace read their memory endpoints
- **THEN** each sees only their own document

### Requirement: Workspace memory endpoint
Workspace members SHALL be able to read the shared `WORKSPACE.md` via `GET /api/v1/workspaces/:ws/memory`; writing via `PUT` SHALL require the workspace settings-management permission (Owner/Admin) and SHALL NOT clobber other workspace fields. PUT past the size cap SHALL be 422.

#### Scenario: Member reads, cannot write
- **WHEN** a Member GETs the workspace memory, then PUTs new content
- **THEN** the read succeeds and the write is rejected 403

#### Scenario: Admin writes shared memory
- **WHEN** an Admin PUTs content to the workspace memory endpoint
- **THEN** the content persists and every agent's next execution composes it

## REMOVED Requirements

### Requirement: Own-memory view and reset
**Reason**: Superseded by the workspace-scoped user memory endpoint (`/me/memory`) and the three-scope memory system; the per-agent-per-user memory model is replaced wholesale.
**Migration**: Clients use `GET/DELETE` → `GET/PUT /api/v1/workspaces/:ws/me/memory`. The underlying `agent_user_memories` table is dropped (it was never writable, so no data exists to migrate).

## MODIFIED Requirements

### Requirement: Runtime-owned writes
Memory content SHALL be written through exactly two paths: the `memory` tool (agent-side, scoped to the executing run) and the two human edit endpoints (own user memory; workspace memory with settings permission). No other management-API endpoint SHALL accept memory content, and regenerating prompts SHALL NOT touch memories.

#### Scenario: No memory write endpoint
- **WHEN** any management-API request other than the two edit endpoints carries memory content
- **THEN** it is ignored; no write path exists outside the memory tool and the two edit endpoints

#### Scenario: Prompt regeneration leaves memory intact
- **WHEN** an agent's IDENTITY/SOUL prompts are regenerated
- **THEN** all three memory scopes are unchanged
