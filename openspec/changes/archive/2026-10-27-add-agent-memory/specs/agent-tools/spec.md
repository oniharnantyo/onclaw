# Spec Delta

## ADDED Requirements

### Requirement: Memory search tool in the catalog

The built-in tool catalog SHALL include a `memory_search` tool (display name "Memory search") that is gated exactly like every other catalog tool: offered to a turn only when enabled globally in the Tools pane and permitted by the agent's tool allowlist. The tool SHALL accept a query and an optional result limit, SHALL search only the session agent's own workspace memory, SHALL make no model call, and SHALL return ranked results or a readable error result that lets the turn continue. It SHALL be offered to subagent child turns under the same gate, and every invocation SHALL be recorded as a recall trace carrying the turn's origin. The tool is read-only: writing memory remains the workspace filesystem tools' job.

#### Scenario: Agent searches its memory

- **WHEN** the model calls `memory_search` with a query that matches one of the agent's notes
- **THEN** the tool result lists the note with its description, path, score, and excerpt, and a recall trace is recorded for the turn

#### Scenario: Other agents' memory is invisible

- **WHEN** the model calls `memory_search` for an agent while another agent holds a note matching the query
- **THEN** the result contains only the session agent's own notes

#### Scenario: Disabled tool is not offered

- **WHEN** `memory_search` is disabled in the Tools pane
- **THEN** subsequent turns of every agent are offered no such tool
