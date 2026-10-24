# Spec Delta — agent-prompts

## ADDED Requirements

### Requirement: Virtual operating contract

Every agent turn SHALL compose a runtime-owned operating contract as a virtual `AGENTS.md` document into the turn's system context, positioned after the agent's persona documents and before the compaction summary. The contract SHALL be maintained in the runtime rather than as a file in any workspace directory, SHALL be present on every turn regardless of the agent's persona document state, and SHALL state that it takes precedence over the persona documents. The contract SHALL cover harness behavior (dangerous shell commands pause for the user's approval and a denial means adjust rather than resubmitting, hook output is feedback, independent tool calls may run in parallel), communication guidance (lead with the outcome, the final message carries everything the user needs), workspace rules (inspect before deleting or overwriting, report outcomes faithfully, do not proactively create report or summary files, refuse destructive or mass-scale abuse), two operating modes — live chat with the user present and autonomous run from a non-user origin — and context-management guidance (continue from the compaction summary rather than wrapping up early).

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
