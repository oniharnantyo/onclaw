## MODIFIED Requirements

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