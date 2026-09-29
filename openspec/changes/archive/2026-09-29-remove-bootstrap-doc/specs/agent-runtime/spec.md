# Spec Delta — agent-runtime

## MODIFIED Requirements

### Requirement: Instruction composition
At execution start, the system instruction SHALL be composed in fixed order from: `AGENTS.md`, `IDENTITY.md`, `SOUL.md`, `WORKSPACE.md`, `USER.md`, `CHANNEL.md`. The L1 base prompt (`AGENTS.md`) SHALL be injected from the platform-embedded template at every composition — it SHALL NOT be read from the agent's workspace directory, so template updates reach every agent on the next execution regardless of workspace age. `IDENTITY.md` and `SOUL.md` SHALL be read from the agent's workspace directory. `WORKSPACE.md` SHALL be rendered from the workspace record (name, description) plus a `## Shared memory` subsection carrying the workspace's shared memory content. `USER.md` SHALL be rendered from the calling user's record and workspace membership (name, email, role) plus a `## Memory` subsection carrying that user's own memory content. `CHANNEL.md` SHALL be rendered only when the execution carries channel context — from the channel record (name, slug, purpose, conventions), the member roster with specialization notes, and the channel catch-up tail as specified in the `agent-channels` capability — and SHALL occupy its fixed position after `USER.md` at the end of the workspace-document tier; executions without channel context SHALL omit it entirely. The memory subsections are distinct from the structured metadata (which remains free context): they carry preferences and information the structured fields do not capture. Composition SHALL happen per execution because `USER.md` varies by caller and memory may have changed since the previous turn, and missing documents and empty memory SHALL be skipped without failing the run (an empty memory omits its subsection entirely). The composed instruction SHALL carry a rich-cards guidance section — the markdown fence conventions for rendering card elements, with one shape per fence tag, plus the composition-tree (`ui`) conventions: the node protocol, the composition vocabulary, the constraint ranges, and the selection rule that composition is reserved for responses whose arrangement carries meaning — in attended, scheduler, and heartbeat compositions, positioned with the `AGENTS.md` base prompt.

#### Scenario: Fixed document order
- **WHEN** an execution composes its instruction with all six documents present
- **THEN** the system instruction contains their contents in the order AGENTS, IDENTITY, SOUL, WORKSPACE, USER, CHANNEL

#### Scenario: Base prompt always current
- **WHEN** the platform-embedded L1 template changes and an agent whose workspace was created before the change executes a turn
- **THEN** the composed instruction carries the new template content, not the agent's historically seeded copy

#### Scenario: Rich cards guidance in every profile
- **WHEN** an attended chat turn, a scheduler execution, and a heartbeat tick each compose their instruction
- **THEN** each composed instruction contains the rich-cards fence conventions

#### Scenario: USER.md varies by caller
- **WHEN** two different members execute the same agent
- **THEN** each execution's instruction carries that member's own name, email, and workspace role

#### Scenario: Missing documents tolerated
- **WHEN** an agent's prompt generation failed and IDENTITY.md/SOUL.md are absent
- **THEN** the execution still runs with the remaining documents (at minimum the injected base prompt plus the two virtual documents)

#### Scenario: Memory rides every turn
- **WHEN** an agent appends to `WORKSPACE.md` during one turn and executes a second turn
- **THEN** the second turn's composed instruction already contains the appended text under the `## Shared memory` subsection

#### Scenario: Empty memory omitted
- **WHEN** a user and workspace have no stored memory
- **THEN** the composed instruction carries the metadata docs without any memory subsections

#### Scenario: Channel execution gains CHANNEL.md
- **WHEN** an agent is summoned from a channel
- **THEN** the composed instruction contains CHANNEL.md — roster, specializations, conventions, catch-up tail — after USER.md, at the end of the workspace-document tier

#### Scenario: Non-channel execution unchanged
- **WHEN** an agent is executed through a direct chat
- **THEN** the composed instruction contains no CHANNEL.md section

#### Scenario: Composition guidance travels with the fence catalog
- **WHEN** any composition profile composes its instruction
- **THEN** the rich-cards section includes the `ui` tree conventions, the component vocabulary with its constraint ranges, and the rule that a single card is rendered by its own tag rather than a composition

#### Scenario: Bootstrap is not composed
- **WHEN** an agent's workspace still contains a `BOOTSTRAP.md` file at execution start
- **THEN** no bootstrap section appears in the composed instruction; the file is swept at startup per the `agent-prompts` capability

### Requirement: Filesystem jail
File tools (list, read, write, edit, glob, grep, delete) and runtime capability middlewares (reduction truncation and context clearing) SHALL operate on virtual mount paths scoped to the agent's workspace directory (mounted at `/workspace`); any resolved path escaping it SHALL be rejected as a tool error, not a crash. The agent's generated prompt documents, its agent-tier skills directory, summarization offload files, and tool reduction offload artifacts (`/workspace/trunc/...` and `/workspace/clear/...`) all live inside this directory and are reachable through the file tools. (Shell execution is no longer banned outright — it is governed by the "Shell execution" and "Dangerous-command approval" requirements below.) Additionally, an agent that is a member of a channel with a project space SHALL have that channel's project directory mounted read-write at `/project` inside its jail (see the `channel-teams` capability); all jail rules apply to `/project` identically.

#### Scenario: Path escape rejected
- **WHEN** a file tool is invoked with a path resolving outside the agent's workspace directory (including via symlink or `..`)
- **THEN** the tool returns an error result and no file outside the directory is read or written

#### Scenario: Delete within the jail
- **WHEN** a file tool deletes a document inside the agent's workspace directory
- **THEN** the file is removed; deleting a missing file is an error result, not a crash

#### Scenario: Shared project root writable for members
- **WHEN** a channel member agent writes `/project/spec.md`
- **THEN** the write succeeds inside the channel's project directory and other member agents read it

#### Scenario: Project root cannot escape the jail
- **WHEN** a member agent follows a symlink in `/project` pointing outside the workspace data root
- **THEN** the resolved path is rejected as a tool error

#### Scenario: Tool reduction offloads under workspace mount
- **WHEN** a tool produces output exceeding the reduction truncation threshold or context clearing triggers
- **THEN** the middleware offloads the content to `/workspace/trunc/<call_id>` or `/workspace/clear/<call_id>` via the jail backend without triggering an absolute-path rejection error, and the agent can read the offloaded file using `read_file`

