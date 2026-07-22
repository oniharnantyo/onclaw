# agent-tools

## Purpose

Provide builtin, workspace-scoped file and shell tools the agent can call, with a shell
execution policy and redaction at the tool-execution boundary.
## Requirements
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

### Requirement: Large tool results spill to a session-scoped file

The system SHALL bound the size of tool results injected into the model context. When a factory
tool's string result exceeds the configured `spill_threshold_bytes` for its tool group, the system
SHALL write the result to a file under the agent workspace and return a small envelope instead of the
verbatim result. The envelope SHALL state the tool name, the result byte size, the configured
threshold, a workspace-relative path to the spilled file, a truncated preview, and an instruction to
use `read_file` with offset/limit to inspect the file. Results at or below the threshold SHALL be
returned inline unchanged. The spill decision SHALL be made by a single decorator applied to every
factory-registered tool, not by per-tool logic. The result SHALL be redacted of recognizable secret
patterns before it is written to disk or included in the preview. The envelope SHALL place the
spill path on a dedicated, unambiguous line so it is machine-detectable by the summarization scrub
(see the conversation-history requirement on preserving spilled-result paths).

#### Scenario: An oversized result is spilled with a recovery hint

- **WHEN** a factory tool returns a result larger than its tool group's `spill_threshold_bytes`
- **THEN** the agent receives an envelope containing a workspace-relative file path, the byte size, and a preview, and the full result is written to that path

#### Scenario: A small result stays inline

- **WHEN** a factory tool returns a result at or below `spill_threshold_bytes`
- **THEN** no spill file is written and the agent receives the result verbatim

#### Scenario: Secrets are redacted before the result reaches disk

- **WHEN** an oversized result contains a recognizable secret pattern
- **THEN** both the spilled file and the envelope preview contain the redacted placeholder, not the secret

#### Scenario: The spill path is on a dedicated, parseable line

- **WHEN** a result is spilled
- **THEN** the envelope contains the spill path as a standalone line, distinct from the preview prose, so the summarization scrub can extract it

### Requirement: The spill threshold is configurable per tool group

The system SHALL expose `spill_threshold_bytes` as a field of each tool-group config schema (Web,
Memory, Browser), resolved at tool-call time so hot-reloaded config is honored, with a per-group
default when unset. The threshold SHALL be independent of `max_bytes`, which bounds downloads and
RAM rather than context injection.

#### Scenario: A configured threshold overrides the default

- **WHEN** a tool group's stored config sets `spill_threshold_bytes` and a tool in that group returns a result exceeding it
- **THEN** the result is spilled

#### Scenario: An unset threshold falls back to the group default

- **WHEN** a tool group's config omits `spill_threshold_bytes`
- **THEN** the per-group default threshold is used

### Requirement: Spilled artifacts are session-scoped and workspace-relative

The system SHALL write spilled results under
`sessions/<session_id>/tool_results/` within the resolved agent workspace, named
`<tool>_<timestamp>_<title>` with a sortable timestamp and a slug derived from the tool's input. The
agent identity SHALL NOT be re-prefixed into the path: the resolved workspace already encodes the
agent (e.g. `~/.onclaw/workspace/<agent>`), so re-appending `.onclaw/workspace/<agent>` would
double-nest the path. The envelope path SHALL be workspace-relative so the workspace-confined
`read_file` can consume it directly. Session, tool, and title components SHALL be sanitized so they
cannot escape the spill directory. Spilled files SHALL persist for the session lifetime.

#### Scenario: The spilled path is consumable by read_file

- **WHEN** a result is spilled and the agent calls `read_file` with the envelope's path
- **THEN** the file contents are returned

#### Scenario: The spill path is not double-nested

- **WHEN** a result is spilled for an agent whose resolved workspace is `~/.onclaw/workspace/<agent>`
- **THEN** the absolute spill directory is `~/.onclaw/workspace/<agent>/sessions/<session_id>/tool_results/` and the envelope path is `sessions/<session_id>/tool_results/<name>`, with no repeated `.onclaw/workspace/<agent>` segment

#### Scenario: Hostile names cannot escape the spill directory

- **WHEN** the agent or session name contains path separators or traversal sequences
- **THEN** the components are sanitized and the file is written under the session's tool_results directory

### Requirement: Each tool-group config category has exactly one schema owner

The system SHALL register at most one config schema per tool-group category. A tool that reads a
knob owned by another tool's category SHALL read it from that category's schema rather than
registering a clobbering schema of its own.

#### Scenario: kg_search no longer clobbers the Memory schema

- **WHEN** the config registry is seeded at startup
- **THEN** the Memory category has exactly one schema (owned by the memory tool), and kg_search reads its `max_depth` from that schema

### Requirement: Filesystem search results are bounded

The filesystem `grep` and `glob` tools SHALL cap the size of their results to protect the model
context, applying the cap at the onclaw-owned backend before the middleware formats the result. When a
result is truncated, the returned set SHALL include a truncation indicator so the agent knows to
narrow its pattern or path rather than assuming the result is complete. This cap SHALL be independent
of the factory-tool spill threshold: filesystem search uses truncation-with-hint (a narrowed re-query
is the appropriate recovery), not file-reference spill.

#### Scenario: A broad grep is truncated with a recovery hint

- **WHEN** `grep` matches more than its result cap allows
- **THEN** the result is capped and includes an indicator that results were truncated, prompting a narrower pattern or path

#### Scenario: A wide glob is truncated with a recovery hint

- **WHEN** `glob` matches more than its entry cap
- **THEN** the result is capped and includes an indicator that entries were truncated, prompting a narrower glob

#### Scenario: A bounded search is returned in full

- **WHEN** `grep`/`glob` results are within their caps
- **THEN** the full result set is returned with no truncation indicator

### Requirement: Image-producing tools persist artifacts and return a path reference

A tool that produces image (or other binary) output (e.g., `browser_screenshot`) SHALL write the raw
bytes to a session-scoped file under the tool_results directory within the resolved agent workspace,
with a binary-appropriate extension (`.png`) and return a path-reference envelope, rather than
injecting a large base64/data-URL string into the model context. The envelope SHALL include the
workspace-relative path (`sessions/<session_id>/tool_results/<name>`). The path SHALL be preserved
across compaction like any other tool_results artifact (it is detected by the same tool_results-path
rule).

#### Scenario: A screenshot is persisted with a path reference

- **WHEN** `browser_screenshot` captures a page image
- **THEN** the raw PNG is written to a `.png` under `sessions/<session_id>/tool_results/` within the resolved agent workspace, and the tool returns an envelope with the workspace-relative path, not a base64 data URL

#### Scenario: The screenshot path survives compaction

- **WHEN** a screenshot envelope in the compacted range references a `.png` under tool_results
- **THEN** the summarizer-input scrub preserves that path verbatim

