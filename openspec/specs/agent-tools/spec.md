# agent-tools

## Purpose

Provide builtin, workspace-scoped file and shell tools the agent can call, with a shell
execution policy and redaction at the tool-execution boundary.
## Requirements
### Requirement: Builtin file tools operate within the workspace

The system SHALL provide `read_file`, `write_file`, `list_dir`, and `edit_file` tools. Every
path the tools touch SHALL be confined to the agent workspace; a path that resolves outside the
workspace (via `..` or an absolute escape) SHALL be rejected. `edit_file` SHALL perform an
exact-string replacement of `old_string` with `new_string`, requiring `old_string` to match
exactly one occurrence in the file; a match of zero or more than one occurrence SHALL be rejected
with no file change.

#### Scenario: Reads and writes inside the workspace succeed

- **WHEN** the agent calls `read_file`/`write_file`/`list_dir`/`edit_file` with a path inside the
  workspace
- **THEN** the operation succeeds against that path

#### Scenario: Escapes outside the workspace are blocked

- **WHEN** the agent calls a file tool with a path that resolves outside the workspace
- **THEN** the tool returns a blocked error and performs no filesystem change

#### Scenario: A unique edit succeeds

- **WHEN** the agent calls `edit_file` with an `old_string` that matches exactly one location in
  the file
- **THEN** only that occurrence is replaced and the file is written

#### Scenario: A non-unique or missing match is rejected

- **WHEN** the agent calls `edit_file` with an `old_string` that matches zero or more than one
  location
- **THEN** the tool rejects the edit with a descriptive error and performs no file change

### Requirement: The shell tool enforces an execution policy

The system SHALL provide a `shell` tool whose execution is gated by a configurable policy of
`deny`, `allowlist`, or `ask`, and a command allowlist. `deny` SHALL block every command;
`allowlist` SHALL allow only allowlisted commands; `ask` SHALL require confirmation before
running. A denied command SHALL return a blocked result with a reason, never executing.

#### Scenario: deny blocks everything

- **WHEN** the policy is `deny` and the agent calls `shell`
- **THEN** no command runs and the tool returns a blocked message

#### Scenario: allowlist gates commands

- **WHEN** the policy is `allowlist` and the agent runs a command not in the allowlist
- **THEN** the command is blocked; a command in the allowlist runs

#### Scenario: ask requires confirmation

- **WHEN** the policy is `ask` in an interactive session and the user declines
- **THEN** the command does not run and the tool returns a blocked message

### Requirement: Secrets are not rehydrated at the tool-execution boundary

The system SHALL sanitize tool arguments before execution and SHALL NOT substitute a real
secret for a redaction placeholder present in tool arguments or results. Known secret
patterns SHALL be masked in tool arguments/results at the dispatch boundary. The masking
SHALL be applied to every builtin tool uniformly via a redaction decorator assembled in the
tool factory, so a newly registered tool inherits masking without per-tool code.

#### Scenario: A redaction placeholder is not rehydrated

- **WHEN** the model emits a tool call whose arguments contain a redaction placeholder
- **THEN** the tool executes with the placeholder passed through unchanged and no plaintext secret is injected

#### Scenario: A secret pattern in tool output is masked

- **WHEN** a tool result contains a known secret pattern (e.g. `sk-...`)
- **THEN** the pattern is masked before the result is returned to the model or recorded

#### Scenario: A newly registered tool inherits redaction

- **WHEN** a new builtin tool is registered through the tool interface
- **THEN** its arguments and results are masked by the same redaction decorator without additional per-tool code

### Requirement: Persona files are edited with the normal file tools

The agent SHALL read and edit its persona/memory files using the same workspace file tools it
uses for any other file (e.g. `write_file`). The system SHALL NOT provide a special
persona-writing tool. File tools are workspace-scoped, so the agent edits the per-agent files
in its workspace; the global `~/.onclaw/USER.md` is outside the workspace and is not
agent-edited during onboarding.

#### Scenario: The agent edits a persona file with write_file

- **WHEN** the agent has learned the user's role during onboarding
- **THEN** it updates `IDENTITY.md` / `USER.md` in its workspace via the normal file tool, with no special tool involved

### Requirement: Memory tools auto-seed into the registry

The `memory_search`, `session_search`, and `memory` tools SHALL auto-seed into `tool_registry` as
enabled by default, following the existing tools-management seeding flow. The tools SHALL inherit
the existing redaction decorator so secrets are masked in their inputs and outputs.

#### Scenario: Memory tools appear in the seeded registry

- **WHEN** the tool registry is seeded
- **THEN** `memory_search`, `session_search`, and `memory` are present and enabled by default

