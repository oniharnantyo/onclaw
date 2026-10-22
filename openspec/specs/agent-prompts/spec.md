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
