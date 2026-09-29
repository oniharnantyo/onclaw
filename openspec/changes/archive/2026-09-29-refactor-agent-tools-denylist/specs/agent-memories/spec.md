# Spec Delta — agent-memories

## MODIFIED Requirements

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
