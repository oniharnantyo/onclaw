# agent-hooks Specification

## Purpose
TBD - created by archiving change add-agent-hooks. Update Purpose after archive.
## Requirements
### Requirement: Lifecycle hook events

The system SHALL fire hook events at five defined points in an agent session: `session_start` (when a session is established), `user_prompt_submit` (before the user's message enters the model pipeline), `pre_tool_use` (before a tool call executes), `post_tool_use` (after a tool call completes), and `stop` (when the session terminates). `user_prompt_submit` and `pre_tool_use` SHALL be blocking events; `session_start`, `post_tool_use`, and `stop` SHALL be non-blocking (observation only).

#### Scenario: A tool call triggers pre and post events

- **WHEN** the agent invokes a tool
- **THEN** the system fires `pre_tool_use` before the tool executes and `post_tool_use` after it completes

#### Scenario: A submitted prompt triggers the user_prompt_submit event

- **WHEN** the user submits a prompt for a turn
- **THEN** the system fires `user_prompt_submit` before the message is sent to the model

### Requirement: Hook decisions and fail-closed semantics

For blocking events, each hook SHALL return an allow or block decision. A single `block` decision SHALL short-circuit the hook chain and prevent the action, with the block reason surfaced to the model so it can adjust (deny-but-continue). If a hook errors, times out subject to its `on_timeout` policy, or cannot produce a decision on a blocking event, the system SHALL default to `block` (fail-closed). Non-blocking events SHALL ignore the decision and continue.

#### Scenario: A blocking hook denies a tool call

- **WHEN** a `pre_tool_use` hook returns `block`
- **THEN** the tool call is not executed and the block reason is provided to the model

#### Scenario: A hook error on a blocking event fails closed

- **WHEN** a `pre_tool_use` hook errors and `on_timeout` is at its default
- **THEN** the system treats the event as blocked

### Requirement: Command handler

The system SHALL support a `command` handler that runs a shell command when its hook matches. The command's standard input SHALL receive a JSON-encoded event payload. The system SHALL interpret the command's exit code as: 0 means allow, 2 means block (with standard error fed back to the model), and any other non-zero value means error. The command SHALL run with only the environment variables listed in its `allowed_env_vars` allowlist plus a small fixed safe set, and SHALL honor the configured working directory.

#### Scenario: Exit code 2 blocks and feeds the reason

- **WHEN** a command hook exits with code 2 and writes a reason to standard error
- **THEN** the action is blocked and the reason is provided to the model

#### Scenario: Secret environment variables are not leaked

- **WHEN** a command hook runs and a configured API key is not in the allowlist
- **THEN** that key is absent from the command's environment

### Requirement: Script handler with embedded JavaScript

The system SHALL support a `script` handler that executes inline JavaScript source in-process via a pure-Go, no-CGO embedded runtime, so that no external interpreter (such as Node.js) is required. The script SHALL expose a `handle(ctx)` function that receives the event context and returns a decision object of the form `{decision: "allow"|"block", reason: "..."}`. A thrown exception SHALL be treated as an error.

#### Scenario: A script hook blocks via its return value

- **WHEN** a `script` hook's `handle(ctx)` returns `{decision: "block", reason: "destructive"}`
- **THEN** the action is blocked and "destructive" is surfaced to the model

#### Scenario: A script runs without an external interpreter

- **WHEN** a `script` hook runs on a machine without Node.js installed
- **THEN** the hook executes successfully via the embedded runtime

### Requirement: Script handler sandboxing

The `script` handler SHALL execute in a sandbox that has no access to the filesystem, network, or module loading unless explicitly bound by the system. The runtime SHALL expose only the read-only event context and a captured `console`.

#### Scenario: A script cannot access the filesystem

- **WHEN** a `script` hook attempts to read or write a file
- **THEN** the sandbox exposes no file API and the attempt cannot succeed

### Requirement: Hook scopes and resolution

Each hook SHALL be scoped to either `global` (applies to all agents) or `agent` (applies to a specific agent). The system SHALL resolve applicable hooks by merging global and agent-scoped hooks for the target agent and ordering them by descending priority. For a blocking event, the chain SHALL stop at the first `block`.

#### Scenario: Global and agent hooks both apply

- **WHEN** a global hook and an agent-scoped hook both match the same event for an agent
- **THEN** both are evaluated in descending priority order

### Requirement: Tool-name matchers

A hook MAY declare a regex matcher applied to the event's tool name. The system SHALL only run the hook's handler when the regex matches the `tool_name` field. A hook without a matcher SHALL run for every occurrence of its event.

#### Scenario: A matcher restricts when a hook fires

- **WHEN** a `pre_tool_use` hook has matcher `^exec$` and the event's tool is `read_file`
- **THEN** the hook is skipped

### Requirement: Hook safeguards

The system SHALL enforce a per-hook timeout (default 5 seconds, maximum 10 seconds) and a per-event chain budget. The system SHALL honor each hook's `on_timeout` policy (`block` or `allow`, default `block` for blocking events). The system SHALL implement a circuit breaker that automatically disables a hook after five blocks or timeouts within a one-minute rolling window.

#### Scenario: A per-hook timeout applies the on_timeout policy

- **WHEN** a command hook runs longer than its timeout and its `on_timeout` is `block`
- **THEN** the event is blocked

#### Scenario: The circuit breaker disables a noisy hook

- **WHEN** a hook produces five blocks within one minute
- **THEN** the system sets the hook's enabled flag to false and stops firing it

### Requirement: Hook storage and audit

The system SHALL persist hook definitions in an `agent_hooks` table and SHALL record every (non-dry-run) hook execution in a `hook_executions` audit table. Deleting a hook SHALL preserve its past execution rows by clearing the audit reference rather than cascading the delete.

#### Scenario: An execution is recorded

- **WHEN** a hook runs
- **THEN** a `hook_executions` row is written capturing the event, handler type, decision, duration, and any error

#### Scenario: Audit rows survive hook deletion

- **WHEN** a hook is deleted after it has executed
- **THEN** its prior audit rows remain

### Requirement: Channel-agnostic session lifecycle

The system SHALL fire `session_start` automatically when an agent's first turn begins, regardless of entry channel — channels only invoke `Run` and consume the event stream; they do not call any session-lifecycle method. The system SHALL fire the `stop` event exactly once per termination: via the agent middleware on normal completion, and via the event stream when a turn terminates with an error. The session's source channel SHALL be supplied at agent assembly and carried in event payloads so hooks can distinguish sources.

#### Scenario: session_start fires on the first turn without a manual call

- **WHEN** a channel calls `Agent.Run` for the first time in a session (from the CLI, the API/Web UI, or any future channel)
- **THEN** `session_start` fires exactly once and no channel code invokes a session-start method

#### Scenario: stop fires once on normal completion

- **WHEN** a turn completes normally
- **THEN** `stop` fires exactly once via the middleware and is not duplicated

#### Scenario: stop fires on turn error via the event stream

- **WHEN** a turn terminates with an error
- **THEN** `stop` fires via the event stream without the channel calling an end-session method

### Requirement: Hook management surface

The system SHALL provide hook management through a CLI (`onclaw hooks add/list/show/remove/toggle/test`), a REST API under `/api/hooks`, and a Web UI panel. Mutations SHALL hot-reload into a running agent. The system SHALL provide a dry-run test that executes a hook against a sample event and returns the decision without writing an audit row.

#### Scenario: The CLI adds a hook

- **WHEN** a user runs `onclaw hooks add --handler command --event pre_tool_use --command '...'`
- **THEN** the hook is persisted and applied to subsequent agent runs

#### Scenario: A dry-run test writes no audit row

- **WHEN** a user runs the test path with a sample event
- **THEN** the decision is returned but no `hook_executions` row is written

### Requirement: Hooks are opt-in

When no hooks are configured for an event, the system SHALL incur no behavioral change to the agent: events fire as no-ops and the agent pipeline is unaffected.

#### Scenario: No hooks means no behavioral change

- **WHEN** an agent runs with no hooks configured
- **THEN** its behavior is identical to an agent running without the hook system

