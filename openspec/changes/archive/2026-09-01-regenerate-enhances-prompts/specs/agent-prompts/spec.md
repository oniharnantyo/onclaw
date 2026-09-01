## MODIFIED Requirements

### Requirement: Regeneration
`POST /workspaces/:ws/agents/:agent/regenerate` (`agents.write`) SHALL re-run generation synchronously from the currently stored brief and MAY carry a JSON body `{instruction}` with the client's requested changes. When the agent's workspace holds generated documents, generation SHALL run in enhance mode: the current IDENTITY.md, SOUL.md, and BOOTSTRAP.md contents are always passed to the model with instructions to strengthen them — preserve the established voice and structure, incorporate the current brief — rather than write from scratch, with or without an instruction; with no documents on disk, generation runs fresh and the instruction rides along as a Requested Changes section. Regeneration SHALL NOT remove or overwrite any existing document until the model output is in hand and backups exist: on success each overwritten document's previous content is preserved as `<name>.bak` beside it before the enhanced content is committed, and a backup failure SHALL abort the commit and fail the generation. On failure the existing documents stay untouched and retryable.

#### Scenario: Regenerate from stored brief
- **WHEN** regeneration is requested for a ready or failed agent
- **THEN** the response returns after generation completes with the refreshed agent (`ready`, or `failed` with the reason)

#### Scenario: Regenerate while in flight
- **WHEN** regenerate is called while status is `generating`
- **THEN** response is 409 conflict

#### Scenario: Regenerate enhances existing documents
- **WHEN** regeneration runs for an agent whose workspace holds generated documents
- **THEN** the model call includes the current documents with enhancement instructions, and on success each previous document is preserved as `<name>.bak` beside the enhanced file

#### Scenario: Change instruction rides along with the old prompts
- **WHEN** regeneration is requested with a non-empty `instruction`
- **THEN** the model call includes the current documents plus the instruction as a Requested Changes section — the old prompts are never dropped in favor of the instruction

#### Scenario: Regenerate without documents runs fresh
- **WHEN** regeneration runs for an agent whose workspace holds no generated documents
- **THEN** the model call carries no existing documents and builds them from the brief alone

#### Scenario: Failed regeneration preserves files
- **WHEN** the model call fails during regeneration
- **THEN** `prompts_status` becomes `failed` with the reason, the previous documents remain on disk unchanged, and no backups are created

#### Scenario: Backup failure aborts the commit
- **WHEN** preserving a previous document as `<name>.bak` fails after a successful model call
- **THEN** the commit is aborted, the original documents remain unchanged on disk, and the generation is marked failed

#### Scenario: Restore from backup
- **WHEN** a user wants the pre-regeneration documents back
- **THEN** the previous version of each file is available as `<name>.bak` beside the document
