# agent-memories Delta

## REMOVED Requirements

### Requirement: Three memory scopes
Agent memory SHALL consist of three independently scoped, append-friendly markdown documents: `USER.md` — memory about the current user, one document per (workspace, user) pair; `WORKSPACE.md` — shared team memory, one document per workspace; `MEMORY-DD-MM-YYYY.md` — the agent's private log for one specific day, one document per (workspace, agent, date). All three SHALL be persisted server-side (database-backed, not files in the agent jail), and every read/write SHALL be scoped to the executing run's workspace, agent, and user — no argument can address another user's, agent's, or workspace's memory. Writing a daily memory for a day that already has one SHALL update that day's document in place; dates SHALL be parsed strictly as dd-mm-yyyy, and the reserved name `MEMORY-TODAY.md` SHALL resolve to today's date in the workspace's timezone.

**Reason**: The daily-log scope is a write-only diary — never injected, never re-read, growing until the cap blocks it. The `agent-memory-pipeline` capability replaces its role: raw conversation history lives in session events, and distilled memory lives in the new notes store.

**Migration**: Operators export `agent_daily_memories` content before upgrading. The table and its storage are dropped; nothing is auto-migrated (the ingestion pipeline re-derives durable facts from session history going forward).

## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Memory tool
Agents with the `memory` tool allowlisted SHALL have exactly one memory tool exposing the two documents through a `path` argument (`USER.md`, `WORKSPACE.md`) and an `action` argument limited to **read** and **append**. `append` SHALL atomically add content to the end of the resolved document (no read-modify-write window) and SHALL require content; `read` SHALL return the document's content, or an explicit empty marker when nothing is stored. There SHALL be no overwrite action — agent-side correction is additive. Addresses outside the two accepted forms SHALL be rejected with an error naming them. Tool failures SHALL return structured error results that do not interrupt the run. Scheduled runs SHALL NOT mount the memory tool.

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
