# agent-memories Specification

## Purpose
Agent general memory as the manually curated tier: two always-injected markdown documents — the caller's `USER.md` and the workspace's `WORKSPACE.md` — written only through the memory tool and the human edit endpoints, with doc-over-notes precedence over extracted memory (see agent-memory-pipeline / agent-memory-retrieval). The former daily-log scope is removed; all memory writes stay runtime-owned — never exposed on the management API beyond the two edit endpoints.

## Requirements

### Requirement: Runtime-owned writes
Memory content SHALL be written through exactly two paths: the `memory` tool (agent-side, scoped to the executing run) and the two human edit endpoints (own user memory; workspace memory with settings permission). No other management-API endpoint SHALL accept memory content, and regenerating prompts SHALL NOT touch memories.

#### Scenario: No memory write endpoint
- **WHEN** any management-API request other than the two edit endpoints carries memory content
- **THEN** it is ignored; no write path exists outside the memory tool and the two edit endpoints

#### Scenario: Prompt regeneration leaves memory intact
- **WHEN** an agent's IDENTITY/SOUL prompts are regenerated
- **THEN** both memory documents are unchanged

### Requirement: Two memory documents
Agent general memory SHALL consist of two independently scoped, append-friendly markdown documents: `USER.md` — memory about the current user, one document per (workspace, user) pair; `WORKSPACE.md` — shared team memory, one document per workspace. Both SHALL be persisted server-side (database-backed) and every read/write SHALL be scoped to the executing run's workspace and user — no argument can address another user's or workspace's document. These documents are the always-injected general-memory tier: whatever they contain is composed into agent instructions every turn without any retrieval step.

#### Scenario: Documents inject every turn
- **WHEN** a run composes agent instructions and the caller's USER.md or the workspace's WORKSPACE.md is non-empty
- **THEN** both documents appear in the composed context verbatim, with no retrieval or gating step

#### Scenario: Scoped to the executing principal
- **WHEN** two members of the same workspace run the same agent
- **THEN** each run composes only that member's USER.md plus the one shared WORKSPACE.md

### Requirement: Doc-over-notes precedence
The two documents are the manually curated tier and SHALL outrank extracted notes wherever both speak: the ingestion pipeline SHALL never overwrite or supersede document content, and an extracted fact that contradicts a document line SHALL be recorded as a review flag for humans instead of being applied.

#### Scenario: Extraction contradicts a document
- **WHEN** the curation gate extracts a fact that contradicts content in USER.md or WORKSPACE.md
- **THEN** the note is stored with a conflict flag for human review and no document content changes

### Requirement: Memory tool
Agents that have not disabled the `memory` tool SHALL have exactly one memory tool exposing the two documents through a `path` argument (`USER.md`, `WORKSPACE.md`) and an `action` argument limited to **read** and **append**. `append` SHALL atomically add content to the end of the resolved document (no read-modify-write window) and SHALL require content; `read` SHALL return the document's content, or an explicit empty marker when nothing is stored. There SHALL be no overwrite action — agent-side correction is additive. Addresses outside the two accepted forms SHALL be rejected with an error naming them. Tool failures SHALL return structured error results that do not interrupt the run. Scheduled runs SHALL NOT mount the memory tool.

#### Scenario: Read empty memory
- **WHEN** an agent reads `USER.md` before anything was stored
- **THEN** the tool returns an explicit empty marker rather than an empty string or an error

#### Scenario: Append without content fails softly
- **WHEN** an agent invokes `append` with no content
- **THEN** the tool returns a structured error result and the run continues

#### Scenario: Append confirmation names the date
- **WHEN** an agent appends to `USER.md` or `WORKSPACE.md`
- **THEN** the result confirms the concrete resolved document by its accepted name

#### Scenario: Daily paths are gone
- **WHEN** an agent addresses `MEMORY-TODAY.md` or any `MEMORY-DD-MM-YYYY.md` path
- **THEN** the tool returns a structured error naming the two accepted forms and stores nothing

### Requirement: Memory size cap
Both memory documents SHALL share a server-defined size cap expressed as a character budget approximating a token budget. The cap SHALL be enforced identically on every write path — the memory tool (as a structured tool error naming the current size, the cap, and the human-trim escape hatch) and the memory HTTP endpoints (422) — and surfaced to UI editors for a live counter. Appends SHALL be checked against the resulting document size, not just the appended fragment.

#### Scenario: Tool append over cap
- **WHEN** an agent appends content that would push a document past the cap
- **THEN** the tool returns a structured error, the stored document is unchanged, and the run continues

#### Scenario: HTTP save over cap
- **WHEN** a user PUTs document content past the cap via an edit endpoint
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
