## MODIFIED Requirements

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

- **WHEN** the session name contains path separators or traversal sequences
- **THEN** the components are sanitized and the file is written under the session's tool_results directory

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
