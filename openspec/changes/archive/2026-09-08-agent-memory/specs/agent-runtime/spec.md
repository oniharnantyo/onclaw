## ADDED Requirements

### Requirement: Birth ritual completion
`BOOTSTRAP.md` SHALL be composed into the instruction only while the file exists in the agent's workspace directory. The agent SHALL be able to remove it through the file tools (deleting `BOOTSTRAP.md` completes the birth sequence described in the document), and removal SHALL be permanent — the file is seeded only when a new agent is created, never re-created afterward.

#### Scenario: Agent completes the birth sequence
- **WHEN** an agent deletes `BOOTSTRAP.md` via a file tool during its first conversation
- **THEN** the deletion succeeds and subsequent executions compose their instruction without the bootstrap document

## MODIFIED Requirements

### Requirement: Instruction composition
At execution start, the system instruction SHALL be composed in fixed order from: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md`, `USER.md`, `BOOTSTRAP.md`. The first three and the last SHALL be read from the agent's workspace directory. `WORKSPACE.md` SHALL be rendered from the workspace record (name, description) plus a `## Shared memory` subsection carrying the workspace's shared memory content. `USER.md` SHALL be rendered from the calling user's record and workspace membership (name, email, role) plus a `## Memory` subsection carrying that user's own memory content. The memory subsections are distinct from the structured metadata (which remains free context): they carry preferences and information the structured fields do not capture. Composition SHALL happen per execution because `USER.md` varies by caller and memory may have changed since the previous turn, and missing documents and empty memory SHALL be skipped without failing the run (an empty memory omits its subsection entirely).

#### Scenario: Fixed document order
- **WHEN** an execution composes its instruction with all six documents present
- **THEN** the system instruction contains their contents in the order AGENTS, IDENTITY, SOUL, WORKSPACE, USER, BOOTSTRAP

#### Scenario: USER.md varies by caller
- **WHEN** two different members execute the same agent
- **THEN** each execution's instruction carries that member's own name, email, and workspace role

#### Scenario: Missing documents tolerated
- **WHEN** an agent's prompt generation failed and IDENTITY.md/SOUL.md/BOOTSTRAP.md are absent
- **THEN** the execution still runs with the remaining documents (at minimum AGENTS.md plus the two virtual documents)

#### Scenario: Memory rides every turn
- **WHEN** an agent appends to `WORKSPACE.md` during one turn and executes a second turn
- **THEN** the second turn's composed instruction already contains the appended text under the `## Shared memory` subsection

#### Scenario: Empty memory omitted
- **WHEN** a user and workspace have no stored memory
- **THEN** the composed instruction carries the metadata docs without any memory subsections

### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep, delete) SHALL operate only on paths inside the agent's workspace directory; any resolved path escaping it SHALL be rejected as a tool error, not a crash. The agent's generated prompt documents, its agent-tier skills directory, and the summarization offload file all live inside this directory and are reachable through the file tools, including deletion of prompt documents such as `BOOTSTRAP.md`. (Shell execution is no longer banned outright — it is governed by the "Shell execution" and "Dangerous-command approval" requirements below.)

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

#### Scenario: Delete within the jail
- **WHEN** a file tool deletes a document inside the agent's workspace directory
- **THEN** the file is removed; deleting a missing file is an error result, not a crash
