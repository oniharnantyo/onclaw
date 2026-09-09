## ADDED Requirements

### Requirement: Hooks pane
The workspace settings SHALL offer a Hooks pane listing the workspace's hooks in evaluation order, each row showing name, event, handler type, a summary of what it selects and whether it can block, subscribed scope, health status (healthy, errored with failure count, disabled), and an enable toggle. Rows SHALL be reorderable by dragging, and the pane SHALL state that hooks evaluate top to bottom with the first block winning. The pane SHALL offer creation and editing through a dialog with, in order: name; event select (with a description of when it fires and whether it can block); a single applies-to field read as a matcher string — empty or `*` selecting every occurrence, exact names and dotted families split on comma, pipe, or whitespace, and anything else an unanchored regular expression — with a live count of how many available values it matches; an optional `if` input-gate field on tool events in `ToolName(pattern)` form; a handler section that swaps by handler type (URL and masked secret headers for webhooks; program, argument rows, and masked secret environment variables for commands; server, tool, and structured input rows with placeholder hints for MCP tools; policy prompt, explicit provider/model pickers, and per-run cap for evaluators; a JavaScript code editor for scripts); a timeout; a failure policy radio (allow default); and an enabled toggle. Saving SHALL surface validation errors per field, including matcher/regex errors, the match count, and script syntax errors with their position. The pane SHALL surface instance-level hooks that reach the workspace in a read-only section, distinguishable from workspace hooks.

#### Scenario: Editing a hook round-trips its selection
- **WHEN** a workspace administrator reopens a hook saved with the matcher string `web.*`
- **THEN** the dialog shows the same string with its live match count

#### Scenario: Health is visible at a glance
- **WHEN** a hook's last delivery attempts failed
- **THEN** its row shows an errored status with the failure count, linking to its execution history

### Requirement: Hook test panel
The Hooks pane SHALL offer a test action that fires a synthetic event at the hook's current configuration, clearly stating that it really executes the handler and records nothing. The result SHALL show a decision badge, duration, and handler-specific detail — exit code and stderr snippet for commands, HTTP status for webhooks, token usage for evaluators, captured console output for scripts — alongside a read-only preview of the event payload that will be sent.

#### Scenario: Test a command hook
- **WHEN** an administrator tests a command hook that blocks the synthetic event
- **THEN** the panel shows a blocked badge with the exit code, duration, and the stderr reason, and the execution history gains no entry

### Requirement: Hook script editor
The `script` handler's configuration SHALL present a JavaScript code editor with syntax highlighting and line numbers. The editor SHALL surface syntax errors inline at their position while the author types, and a script with a syntax error SHALL NOT be submittable. The editor SHALL offer a Format action that beautifies the script in place without changing its meaning.

#### Scenario: Typo surfaces inline
- **WHEN** the author types a script containing a syntax error
- **THEN** the editor marks the offending position with the parser's message

#### Scenario: Parse error blocks save
- **WHEN** the author submits a hook whose script does not parse
- **THEN** the save is blocked client-side before any request is sent

#### Scenario: Format beautifies
- **WHEN** the author invokes Format
- **THEN** the script is reformatted in place with equivalent code

### Requirement: Hook execution history
The Hooks pane SHALL provide a per-hook execution history view listing time, event, decision, duration, and failure detail for each recorded evaluation, including records preserved from hooks that have since been deleted.

#### Scenario: Inspect a blocked call
- **WHEN** an administrator opens the history for a gate hook that blocked a tool call earlier that day
- **THEN** the corresponding execution is listed with its decision, duration, and the reason recorded
