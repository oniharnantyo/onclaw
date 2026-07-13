## MODIFIED Requirements

### Requirement: Builtin file tools operate within the workspace

The system SHALL provide `read_file`, `write_file`, `ls`, `edit_file`, `glob`, and `grep` tools.
Every path the tools touch SHALL be confined to the agent workspace; a path that resolves outside
the workspace (via `..` or an absolute escape) SHALL be rejected. `edit_file` SHALL perform an
exact-string replacement of `old_string` with `new_string`, requiring `old_string` to match exactly
one occurrence in the file; a match of zero or more than one occurrence SHALL be rejected with no
file change. `glob` SHALL enumerate workspace paths matching a glob expression. `grep` SHALL search
workspace file contents against a pattern and return matching lines with optional surrounding
context. The file tools SHALL be provided by the Eino filesystem middleware backed by an
onclaw-controlled `Backend` (see "File and shell tools are provided by the Eino filesystem
middleware").

#### Scenario: Reads and writes inside the workspace succeed

- **WHEN** the agent calls `read_file`/`write_file`/`ls`/`edit_file` with a path inside the
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

#### Scenario: glob enumerates matching workspace paths

- **WHEN** the agent calls `glob` with a pattern (e.g. `**/*.go`)
- **THEN** the workspace paths matching the pattern are returned, and no path outside the workspace
  appears in the result

#### Scenario: grep returns matching content with context

- **WHEN** the agent calls `grep` with a pattern and optional context-line counts
- **THEN** matching lines (with requested context) from workspace files are returned, and secret
  patterns in the matched content are masked

### Requirement: The shell tool enforces an execution policy

The system SHALL provide an `execute` tool whose execution is gated by a configurable policy of
`deny`, `allowlist`, `ask`, or `denylist`, together with a command allowlist and a command denylist.
The **default policy SHALL be `denylist`**. The `execute` tool SHALL be a pass-through that delegates
command execution to the shell implementation; it SHALL NOT itself enforce policy.

- `deny` SHALL block every command.
- `allowlist` SHALL allow only commands whose leading binary is in the allowlist.
- `ask` SHALL require interactive confirmation before running (CLI/stdin only).
- `denylist` SHALL allow every command **except** those matching a catastrophic pattern, evaluated
  against the **entire command string** (not only the leading token).

Pattern evaluation SHALL consider the **full command string** so that shell composition — pipes
(`|`), sequencing (`&&`, `;`), redirection, and command substitution — is visible to the policy. A
command blocked by any policy SHALL NOT execute and SHALL return a blocked result that names the
reason; each `denylist` match SHALL additionally be logged.

#### Scenario: deny blocks everything

- **WHEN** the policy is `deny` and the agent calls `execute`
- **THEN** no command runs and the tool returns a blocked message

#### Scenario: allowlist gates commands

- **WHEN** the policy is `allowlist` and the agent runs a command not in the allowlist
- **THEN** the command is blocked; a command in the allowlist runs

#### Scenario: ask requires confirmation

- **WHEN** the policy is `ask` in an interactive session and the user declines
- **THEN** the command does not run and the tool returns a blocked message

#### Scenario: denylist blocks a catastrophic command and names the reason

- **WHEN** the policy is `denylist` and the agent runs a command matching a catastrophic pattern
  (e.g. `rm -rf /`, `curl … | sh`)
- **THEN** the command is blocked with a reason naming the matched category, it does not execute,
  and the match is logged

#### Scenario: execute delegates to the shell without enforcing policy itself

- **WHEN** the agent calls `execute`
- **THEN** the command is handed to the shell implementation, which applies the configured policy;
  the `execute` tool does not independently allow or block the command

### Requirement: Secrets are not rehydrated at the tool-execution boundary

The system SHALL sanitize tool arguments before execution and SHALL NOT substitute a real secret for
a redaction placeholder present in tool arguments or results. Known secret patterns SHALL be masked
in tool arguments/results at the dispatch boundary. For builtin tools assembled through the tool
factory, masking SHALL be applied uniformly via a redaction decorator so a newly registered tool
inherits masking without per-tool code. For the file and shell tools injected by the Eino filesystem
middleware (which bypass the tool factory), masking SHALL be applied inside the onclaw-controlled
`Backend` and `Shell` implementations — in returned file content, grep matches, and command output —
so the same no-plaintext-secret invariant holds across both injection paths.

#### Scenario: A redaction placeholder is not rehydrated

- **WHEN** the model emits a tool call whose arguments contain a redaction placeholder
- **THEN** the tool executes with the placeholder passed through unchanged and no plaintext secret is injected

#### Scenario: A secret pattern in tool output is masked

- **WHEN** a tool result contains a known secret pattern (e.g. `sk-...`)
- **THEN** the pattern is masked before the result is returned to the model or recorded

#### Scenario: A newly registered tool inherits redaction

- **WHEN** a new builtin tool is registered through the tool interface
- **THEN** its arguments and results are masked by the same redaction decorator without additional per-tool code

#### Scenario: File and shell tools mask secrets despite bypassing the factory decorator

- **WHEN** a file read, grep, or command execution returns content containing a known secret pattern
- **THEN** the pattern is masked by the `Backend`/`Shell` implementation before the result is returned to the model

## ADDED Requirements

### Requirement: File and shell tools are provided by the Eino filesystem middleware

The `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, and `execute` tools SHALL be
provided by the Eino `filesystem` ADK middleware, backed by onclaw-controlled `filesystem.Backend`
and `filesystem.Shell` implementations. The middleware SHALL be constructed as a typed middleware
matching the agent's message type and added to the agent's handler chain. The system SHALL accept
the middleware's default tool names and SHALL NOT rename them. The `Backend` SHALL confine every
path to the agent workspace and mask secret patterns in returned content; the `Shell` SHALL enforce
the shell execution policy and mask secret patterns in command output. The middleware supersedes
the prior hand-rolled registry implementations of these tools, which SHALL be removed.

#### Scenario: The filesystem middleware is wired into the agent

- **WHEN** an agent is assembled
- **THEN** the `filesystem` middleware is a member of the agent's handler chain, supplying the
  `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`, and `execute` tools

#### Scenario: File and shell semantics stay under onclaw control

- **WHEN** the agent invokes any of the seven middleware-provided tools
- **THEN** path confinement, secret masking, and (for `execute`) the shell execution policy are
  enforced by onclaw's `Backend`/`Shell` implementations, not by the middleware's defaults
