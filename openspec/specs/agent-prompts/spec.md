# agent-prompts Specification

## Purpose

Give each agent a file-backed persona: an IDENTITY, SOUL, and BOOTSTRAP document generated from the agent's brief on its own provider and model, composed into every turn, with a visible status machine and enhance-mode regeneration — ported from onclaw-v2's agent-prompts capability.

## Requirements

### Requirement: File-backed prompt documents

Each agent SHALL have three prompt documents — `IDENTITY.md`, `SOUL.md`, and `BOOTSTRAP.md` — stored as files in the agent's workspace directory. The documents SHALL be composed onto the agent on every read and every turn, and SHALL be absent from the database. Editing a document through the agent update path SHALL rewrite the corresponding file.

#### Scenario: Documents live on disk, not in the database

- **WHEN** an agent's documents are read
- **THEN** the content comes from the workspace directory files and no document content column exists in the database

#### Scenario: Edit rewrites the file

- **WHEN** the user saves an edited SOUL document
- **THEN** `SOUL.md` in the agent's workspace directory contains the new content and subsequent turns use it

### Requirement: Generation from the brief

Creating an agent SHALL generate its three documents synchronously from the stored brief before the create call completes, using the agent's own configured provider and model with hardcoded generation parameters and a bounded timeout. The brief SHALL be persisted at create so regeneration works without client state. A generation failure SHALL surface a clear error and the agent's prompt status SHALL read `failed`.

#### Scenario: Create completes only after generation

- **WHEN** the user submits the create wizard with a valid brief
- **THEN** the create call returns with the prompt status `ready` and the three documents exist in the workspace directory

#### Scenario: Unreachable provider fails the generation

- **WHEN** generation runs for an agent whose provider cannot be reached
- **THEN** the create call reports the failure, the prompt status reads `failed` with the reason, and the agent row still exists and stays editable

### Requirement: Prompt status machine and startup sweep

The agent's prompt status SHALL move `generating → ready | failed` only. A startup sweep SHALL move stale `generating` rows (a crash mid-generation) to `failed` with the error "prompt generation interrupted — retry". Agents in `failed` SHALL remain fully editable, listed, and usable in chat, with retry available.

#### Scenario: Interrupted by restart

- **WHEN** the app dies mid-generation and starts again
- **THEN** the affected agent reads `failed` with the interrupted error and no row is stuck in `generating`

#### Scenario: Failed agent stays usable

- **WHEN** prompt generation fails for an agent
- **THEN** the agent stays editable and listed, and its chat works

### Requirement: Turn composition from documents

Every turn SHALL compose the agent's identity, soul, and bootstrap documents as the agent's persona context. Document changes SHALL apply to subsequent turns without requiring an app restart. Missing documents SHALL degrade cleanly: an agent without generated documents still runs turns, just without the persona documents.

#### Scenario: Document change applies to the next turn

- **WHEN** the user edits an agent's SOUL document and sends a message in its chat
- **THEN** the turn's persona context reflects the new content

#### Scenario: Agent without documents still chats

- **WHEN** an agent whose generation failed receives a message
- **THEN** the turn executes without persona documents and the transcript records it normally

### Requirement: Regeneration

The user SHALL be able to regenerate an agent's documents synchronously from the currently stored brief, optionally with a change instruction. When the workspace holds generated documents, generation SHALL run in enhance mode: the current document contents are always passed to the model with instructions to strengthen them — preserving the established voice and structure and incorporating the current brief — with or without an instruction. With no documents on disk, generation SHALL run fresh and the instruction SHALL ride along as a requested-changes section. Before committing enhanced content, each overwritten document's previous content SHALL be preserved as `<name>.bak` beside it, and a backup failure SHALL abort the commit and fail the generation. On failure the existing documents SHALL stay untouched and retryable. Regeneration while a generation is already in flight SHALL be rejected as a conflict.

#### Scenario: Regenerate from the stored brief

- **WHEN** regeneration is requested for a ready or failed agent
- **THEN** the call returns after generation completes with the refreshed status and documents

#### Scenario: Regenerate while in flight

- **WHEN** regeneration is requested while the prompt status is `generating`
- **THEN** the call is rejected as a conflict

#### Scenario: Enhance keeps a backup

- **WHEN** regeneration succeeds for an agent whose workspace holds generated documents
- **THEN** each overwritten document's previous content is present as `<name>.bak` beside the enhanced file

#### Scenario: Backup failure aborts the commit

- **WHEN** a backup cannot be written during regeneration
- **THEN** the generation fails and every existing document keeps its pre-regeneration content

#### Scenario: Instruction rides along with the old prompts

- **WHEN** regeneration is requested with a non-empty instruction
- **THEN** the model call includes the current documents plus the instruction as a requested-changes section — the old prompts are never dropped in favor of the instruction

### Requirement: Virtual operating contract

Every agent turn SHALL compose a runtime-owned operating contract as a virtual `AGENTS.md` document into the turn's system context, positioned after the agent's persona documents and before the compaction summary. The contract SHALL be maintained in the runtime rather than as a file in any workspace directory, SHALL be present on every turn regardless of the agent's persona document state, and SHALL state that it takes precedence over the persona documents. The contract SHALL cover harness behavior (dangerous shell commands pause for the user's approval and a denial means adjust rather than resubmitting, hook output is feedback, independent tool calls may run in parallel), communication guidance (lead with the outcome, the final message carries everything the user needs), workspace rules (inspect before deleting or overwriting, report outcomes faithfully, do not proactively create report or summary files, write deliverables the user should find — reports, generated documents, exported data — under `artifacts/` at the workspace root where files that appear or change during the turn are shown to the user with the reply, refuse destructive or mass-scale abuse), two operating modes — live chat with the user present and autonomous run from a non-user origin — and context-management guidance (continue from the compaction summary rather than wrapping up early).

#### Scenario: Contract present without persona documents

- **WHEN** an agent whose prompt generation failed receives a message
- **THEN** the turn's system context includes the operating contract even though no persona documents exist

#### Scenario: Contract is virtual

- **WHEN** any agent's workspace directory is inspected
- **THEN** no `AGENTS.md` file exists on disk — the contract exists only in the composed system context

#### Scenario: Persona changes leave the contract unchanged

- **WHEN** an agent's persona documents are edited or regenerated
- **THEN** the composed operating contract is identical to before the change

#### Scenario: Contract states precedence

- **WHEN** the operating contract is composed into a turn
- **THEN** it includes the statement that it takes precedence over the persona documents where they conflict

#### Scenario: Contract names the deliverables home

- **WHEN** the operating contract is composed into a turn
- **THEN** its workspace rules state that user-facing deliverables are written under `artifacts/` and that files appearing or changing there are shown to the user with the reply

### Requirement: Turn environment block

Every agent turn SHALL compose an environment block into the system context after the compaction summary stating the effective provider and model, the current date, the turn's origin, and whether the user is present. The block SHALL NOT duplicate workspace-mount guidance that the filesystem capability already supplies.

#### Scenario: Autonomous origin marked absent

- **WHEN** a turn is dispatched from a scheduler, webhook, or channel origin
- **THEN** the environment block names that origin and states that the user is not present

#### Scenario: Chat origin marked present

- **WHEN** a turn is dispatched from the user's chat
- **THEN** the environment block names the user origin and states that the user is present

#### Scenario: Current date is stated

- **WHEN** any turn is composed
- **THEN** the environment block states the current date

### Requirement: Subagent child operating notes

Subagent child turns SHALL compose a reduced operating contract into the child's system context directing findings into the child's final reply rather than report, summary, or notes files, and requiring workspace-safe absolute paths. The full operating contract's user-facing operating-mode guidance SHALL NOT be composed into child turns.

#### Scenario: Child turn carries the reduced notes

- **WHEN** a subagent child turn is composed
- **THEN** the child's system context includes the reduced operating notes

#### Scenario: Child does not inherit operating modes

- **WHEN** a subagent child turn is composed
- **THEN** the live-chat and autonomous-run mode guidance is absent from the child's system context
