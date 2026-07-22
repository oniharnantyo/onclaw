## ADDED Requirements

### Requirement: Large tool results spill to a session-scoped file
The system SHALL bound the size of tool results injected into the model context. When a factory
tool's string result exceeds the configured `spill_threshold_bytes` for its tool group, the system
SHALL write the result to a file under the agent workspace and return a small envelope instead of the
verbatim result. The envelope SHALL state the tool name, the result byte size, the configured
threshold, a workspace-relative path to the spilled file, a truncated preview, and an instruction to
use `read_file` with offset/limit to inspect the file. Results at or below the threshold SHALL be
returned inline unchanged. The spill decision SHALL be made by a single decorator applied to every
factory-registered tool, not by per-tool logic. The result SHALL be redacted of recognizable secret
fpatterns before it is written to disk or included in the preview. The envelope SHALL place the
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
`.onclaw/workspace/<agent>/sessions/<session_id>/tool_results/` within the resolved workspace, named
`<tool>_<timestamp>_<title>` with a sortable timestamp and a slug derived from the tool's input. The
envelope path SHALL be workspace-relative so the workspace-confined `read_file` can consume it
directly. Agent, session, tool, and title components SHALL be sanitized so they cannot escape the
spill directory. Spilled files SHALL persist for the session lifetime.

#### Scenario: The spilled path is consumable by read_file
- **WHEN** a result is spilled and the agent calls `read_file` with the envelope's path
- **THEN** the file contents are returned

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
bytes to a session-scoped file under the tool_results directory with a binary-appropriate extension
(`.png`) and return a path-reference envelope, rather than injecting a large base64/data-URL string
into the model context. The envelope SHALL include the workspace-relative path. The path SHALL be
preserved across compaction like any other tool_results artifact (it is detected by the same
tool_results-path rule).

#### Scenario: A screenshot is persisted with a path reference
- **WHEN** `browser_screenshot` captures a page image
- **THEN** the raw PNG is written to a `.png` under `.onclaw/workspace/<agent>/sessions/<session_id>/tool_results/` and the tool returns an envelope with the workspace-relative path, not a base64 data URL

#### Scenario: The screenshot path survives compaction
- **WHEN** a screenshot envelope in the compacted range references a `.png` under tool_results
- **THEN** the summarizer-input scrub preserves that path verbatim
