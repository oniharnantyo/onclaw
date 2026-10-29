# Spec Delta

## MODIFIED Requirements

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
