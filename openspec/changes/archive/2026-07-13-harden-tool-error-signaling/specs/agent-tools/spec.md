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

A rejection by any file tool — a path that resolves outside the workspace, a missing file or
directory, a non-unique or absent `edit_file` match, or an invalid/empty `grep`/`glob` pattern —
SHALL be returned as a **tool-result observation** (a human-readable result with no fatal error) so
the agent turn continues; see "Builtin tool expected failures are recoverable observations".

#### Scenario: Reads and writes inside the workspace succeed

- **WHEN** the agent calls `read_file`/`write_file`/`ls`/`edit_file` with a path inside the
  workspace
- **THEN** the operation succeeds against that path

#### Scenario: Escapes outside the workspace are blocked as recoverable observations

- **WHEN** the agent calls a file tool with a path that resolves outside the workspace
- **THEN** the tool returns a human-readable blocked observation naming the requested path, performs
  no filesystem change, and returns no fatal error so the agent turn continues

#### Scenario: A unique edit succeeds

- **WHEN** the agent calls `edit_file` with an `old_string` that matches exactly one location in
  the file
- **THEN** only that occurrence is replaced and the file is written

#### Scenario: A non-unique or missing match is rejected as a recoverable observation

- **WHEN** the agent calls `edit_file` with an `old_string` that matches zero or more than one
  location
- **THEN** the tool returns a human-readable rejection naming the reason, performs no file change,
  and returns no fatal error so the agent turn continues

#### Scenario: glob enumerates matching workspace paths

- **WHEN** the agent calls `glob` with a pattern (e.g. `**/*.go`)
- **THEN** the workspace paths matching the pattern are returned, and no path outside the workspace
  appears in the result

#### Scenario: grep returns matching content with context

- **WHEN** the agent calls `grep` with a pattern and optional context-line counts
- **THEN** matching lines (with requested context) from workspace files are returned, and secret
  patterns in the matched content are masked

## ADDED Requirements

### Requirement: Builtin tool expected failures are recoverable observations

Every builtin tool SHALL distinguish **expected failures** from **infrastructure failures** by
their signaling channel. An expected failure — the tool declining, not finding, rejecting invalid
input, or hitting a transient external condition — SHALL be returned as a tool-result observation
with `nil` error, so the agent loop receives an observation and the turn continues within its
`max_iterations` budget. Only a genuine infrastructure failure SHALL be returned as a fatal Go
error that terminates the turn. This contract applies uniformly across the filesystem, memory,
knowledge-graph, web, and browser tools; family-specific requirements in the respective specs
elaborate the conditions for each.

Context cancellation (`context.Canceled`, `context.DeadlineExceeded`) SHALL be propagated and SHALL
NOT be converted to an observation, in any tool. The `execute` shell tool already follows this
contract and is unchanged.

#### Scenario: Invalid tool input does not terminate the turn

- **WHEN** the agent calls a tool with missing or invalid required input (e.g. `kg_search` with an
  empty `seed_entity_name`)
- **THEN** the tool returns a human-readable observation describing the invalid input with no fatal
  error, and the agent turn continues

#### Scenario: An external-service failure does not terminate the turn

- **WHEN** the agent calls a tool whose backing service fails transiently (e.g. `web_fetch` over an
  unreachable URL, or a browser navigation timeout)
- **THEN** the tool returns a human-readable observation naming the target and the reason with no
  fatal error, and the agent turn continues

#### Scenario: A resource-not-found condition does not terminate the turn

- **WHEN** the agent calls a tool for a resource that does not exist or is not unique (e.g.
  `read_file` on a missing path, an `edit_file` non-unique match, a browser element reference that
  is not found)
- **THEN** the tool returns a human-readable observation with no fatal error, and the agent turn
  continues

#### Scenario: An infrastructure failure terminates the turn

- **WHEN** a tool operation fails for a reason that is not an expected failure (e.g. an
  unrecoverable I/O error)
- **THEN** the tool returns a fatal Go error and the agent turn stops

#### Scenario: Context cancellation is propagated

- **WHEN** a tool operation is in flight and the context is cancelled or its deadline expires
- **THEN** the cancellation is returned as a fatal error and is not converted to an observation
