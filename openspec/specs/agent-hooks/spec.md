# agent-hooks Specification

## Purpose

Lifecycle hooks for agent runs: workspace- and instance-defined handlers that observe or block turns and tool calls at fixed lifecycle events, with one uniform decision contract across four handler types, three governance levels, full audit, and health surfacing.

## Requirements

### Requirement: Hook event catalog
The system SHALL support exactly five hook events in v1: `run_started` (observational), `user_prompt_submit` (blocking), `pre_tool_use` (blocking), `post_tool_use` (observational), and `run_finished` (observational). `run_finished` SHALL fire on every terminal outcome and carry a `status` value of `completed`, `failed`, or `cancelled`. Every event payload SHALL carry an `origin` value of `user`, `cron`, or `channel` identifying what triggered the run. Event payloads SHALL identify the workspace, agent, session, originating user, and — for tool events — the tool name, call id, and arguments.

#### Scenario: Run finished covers failure
- **WHEN** a run ends because the model provider returned an error
- **THEN** `run_finished` hooks fire once with `status` of `failed`

#### Scenario: Cron runs are gated
- **WHEN** a schedule-triggered run submits its prompt and a `user_prompt_submit` hook applies to origin `cron`
- **THEN** the hook evaluates before the model is called

### Requirement: Blocking decision semantics
A blocking event SHALL evaluate all matching hooks in order and stop at the first `block` decision; the blocking hook's `reason` SHALL be the one reported. A `pre_tool_use` block SHALL be delivered to the model as the tool call's result payload (identifying the hook and reason) — the run MUST continue with that result and MUST NOT surface the block as a run failure or as a human approval interrupt. A `user_prompt_submit` block SHALL end the turn with a visible notice before the model is called, and the notice SHALL persist so reloaded transcripts show why the turn has no assistant reply. A blocked prompt or tool call MUST still produce a well-formed stream termination for live consumers.

#### Scenario: Tool call blocked by hook
- **WHEN** a `pre_tool_use` hook returns `block` with a reason for a tool the agent called
- **THEN** the tool does not execute, the model receives the block reason as the tool result, the run continues, and the transcript shows the blocked call with the reason

#### Scenario: Prompt blocked before the model
- **WHEN** a `user_prompt_submit` hook blocks a submitted message
- **THEN** the turn ends with a notice carrying the reason, no model call occurs, and a page reload shows the notice in the transcript

### Requirement: No double evaluation across approval resume
When a tool call that required human approval is approved and re-executes, `pre_tool_use` hooks MUST NOT evaluate that call a second time within the same run; the decision recorded before the approval interrupt SHALL stand.

#### Scenario: Approved shell command fires once
- **WHEN** a shell command is blocked-and-allowed by policy, held for human approval, approved, and re-executed in the same run
- **THEN** `pre_tool_use` hooks have evaluated the call exactly once

### Requirement: Observational hooks never affect the run
`run_started`, `post_tool_use`, and `run_finished` hooks MUST NOT delay, fail, or cancel the run regardless of handler latency, errors, or timeouts. Their deliveries MUST survive run teardown and cancellation long enough to complete or time out on their own budget, and a handler panic MUST be contained.

#### Scenario: Dead endpoint does not slow the run
- **WHEN** a `run_finished` hook points at an unreachable URL with a 5 second timeout
- **THEN** the run's terminal event and stream are unaffected, and the delivery attempt is recorded as failed after its own timeout

### Requirement: Hook matcher
Each hook SHALL carry a single matcher string interpreted against an event-aware value: the tool name for `pre_tool_use` and `post_tool_use`, the `origin` for `run_started` and `user_prompt_submit`, and the `status` for `run_finished`. The string SHALL be read with a tiered rule (Claude Code's interpretation adapted to dotted tool names): an empty string or `*` SHALL select every occurrence; otherwise the string SHALL be split on commas, pipes, and whitespace, and if every entry is an exact value or a trailing-`.*` family (e.g. an entire browser toolset or one MCP server's tools) the entries SHALL match exactly or by family; if any entry falls outside that charset, the whole string SHALL be treated as an unanchored regular expression capped in length that MUST compile on a linear-time engine. There is no negation form: selecting "all except" a set SHALL be expressed by listing the selected values instead. Save-time validation MUST reject un-compilable or over-long regex-tier matchers with a field-level error, and MUST report how many of the workspace's currently available values the matcher selects; the editing UI SHALL surface the same count live. Hooks whose matcher does not select the current occurrence MUST be skipped entirely — no handler execution and no audit record.

#### Scenario: Notify only on failures
- **WHEN** a `run_finished` hook has matcher `failed` and a run completes successfully
- **THEN** the hook does not execute and no execution record is written

#### Scenario: Family versus exact name
- **WHEN** a `pre_tool_use` hook has matcher `mcp__github.*` and a tool from that server is invoked
- **THEN** the hook executes; a matcher of `mcp__github` (exact) would not have selected it

#### Scenario: Invalid pattern rejected at save
- **WHEN** a hook is saved whose matcher falls into the regex tier and does not compile
- **THEN** the save is rejected with a field-level error naming the pattern problem

#### Scenario: Match count at save time
- **WHEN** a `pre_tool_use` hook is saved with matcher `browser.*` selecting 4 browser tools out of 24 available
- **THEN** the save response reports 4 selected of 24

### Requirement: Input-level `if` condition
A `pre_tool_use` or `post_tool_use` hook MAY carry an optional `if` condition in the rule form `ToolName(pattern)`: the name part SHALL follow the matcher's exact-name/family entry rules, and the pattern SHALL be a non-empty, length-capped regular expression that compiles on a linear-time engine, matched unanchored against the serialized tool input. The hook SHALL execute only when the tool call's name matches the name part AND the input matches the pattern; otherwise the hook SHALL be skipped entirely with no execution and no audit record. An `if` condition SHALL never block by itself. Other events SHALL NOT accept an `if` condition, and save-time validation MUST reject a malformed or un-compilable one with a field-level error.

#### Scenario: Input gate narrows a tool hook
- **WHEN** a `pre_tool_use` hook with matcher `read_file` and `if` `read_file(secret.*)` fires for a call whose arguments contain no matching path
- **THEN** the hook is skipped with no execution record and the tool call proceeds unaffected

#### Scenario: Malformed if rejected at save
- **WHEN** a hook is saved with an unbalanced `if` such as `read_file(` on a tool event, or any `if` on `run_started`
- **THEN** the save is rejected with a field-level error

### Requirement: Handler registry and unified decision contract
Hooks SHALL execute through handler types registered in a handler registry; v1 SHALL ship `http`, `command`, `mcp_tool`, and `prompt`. Every handler type SHALL accept the same event payload and MAY return the same decision object (`{"decision": "allow"|"block", "reason"}`); a successful execution that returns no decision object SHALL mean allow. Any handler failure — timeout, spawn or connection error, malformed or absent structured output where one is required — SHALL resolve to the hook's failure policy: `allow` (the default) or `block`. Each hook SHALL carry its own timeout budget.

#### Scenario: Failure policy applies uniformly
- **WHEN** a hook's handler exceeds its timeout during a blocking event and the hook's failure policy is `block`
- **THEN** the action is blocked with a failure reason, as if the handler had returned an explicit block

### Requirement: HTTP handler
The `http` handler SHALL POST the event payload as JSON to the hook's URL with headers identifying the event type and a unique delivery id, plus any configured headers. A 2xx response body that parses as a decision object SHALL be honored; any other 2xx response SHALL mean allow. Configured header values SHALL be stored encrypted and SHALL never be returned by any read endpoint; edits SHALL preserve stored values unless replaced. Outbound webhook requests SHALL be blocked from resolving to loopback, private, or link-local addresses, including across redirects, reusing the platform's existing outbound-fetch guards.

#### Scenario: Webhook block honored
- **WHEN** a `pre_tool_use` HTTP hook responds 200 with a body blocking the action and giving a reason
- **THEN** the action is blocked and the response reason is shown in the transcript and audit record

#### Scenario: Secrets never echoed
- **WHEN** a workspace member reads a hook that has a configured Authorization header
- **THEN** the response shows only a masked hint of the stored value

### Requirement: Command handler
The `command` handler SHALL execute a configured program (with an argument list, never a shell string) as the server's OS user, delivering the event payload as JSON on standard input and closing it. Exit code 0 SHALL mean allow unless standard output parses entirely as a decision object, which SHALL be honored; exit code 2 SHALL mean block with standard error (truncated) as the reason, or a default reason when standard error is empty; exit code 2 MUST NOT be overridden by a standard-output decision; any other exit code, a signal kill, a spawn failure, or a timeout SHALL be a failure under the hook's failure policy. The process environment SHALL contain only a minimal fixed base plus the hook's configured variables — the server's own environment, including database and signing secrets, MUST NOT be inherited. On timeout the process MUST be terminated (first gracefully, then forcefully) so no child survives holding the pipes. A configured program that cannot be found at save time SHALL produce a warning, not a rejection. Deployments MAY disable the command handler entirely via an instance setting; when disabled, command hooks SHALL fail validation on save and be skipped at runtime.

#### Scenario: Existing exit-2 script blocks unchanged
- **WHEN** a hook runs a script that prints a reason to standard error and exits 2
- **THEN** the action is blocked with that reason, exactly as the same script behaves under other agent harnesses

#### Scenario: Server secrets never leak to a hook
- **WHEN** a command hook enumerates its environment during execution
- **THEN** it sees only the fixed base variables plus its own configured variables, and no database URL or signing key appears

#### Scenario: Hung script is killed
- **WHEN** a command hook exceeds its timeout
- **THEN** the process tree is terminated within a bounded grace period and the failure policy applies

### Requirement: MCP tool handler
The `mcp_tool` handler SHALL invoke a tool on a workspace-level MCP server, substituting a closed set of event placeholders (tool name, tool argument fields, agent name, origin) into the configured structured input. Only workspace-level MCP servers SHALL be selectable; agent-private servers MUST be rejected at save. If the tool's text result parses as a decision object it SHALL be honored; otherwise a successful tool call SHALL mean allow and a failed tool call SHALL follow the failure policy.

#### Scenario: Notification via MCP tool
- **WHEN** an observational hook invokes a messaging MCP tool whose input references the blocked tool's name
- **THEN** the tool is invoked with the substituted text and the hook records a successful delivery

### Requirement: Prompt (LLM evaluator) handler
The `prompt` handler SHALL evaluate a matching tool call by asking a configured model to decide, using a workspace-provided policy prompt. The evaluator MUST receive only the tool name and tool arguments — never the user's message or conversation history — with the arguments presented as delimited untrusted data. The evaluator MUST return its verdict exclusively through a structured decision tool whose fields are the decision, a reason, and an injection flag; a response without a structured verdict SHALL be treated as evaluator failure; an injection flag SHALL force a block regardless of the verdict. The hook configuration MUST name its provider and model explicitly and MUST have a matcher that does not select every occurrence (non-empty and not `*`); saving a prompt hook with a match-all matcher MUST be rejected. Evaluation SHALL be capped per run (default 5); occurrences beyond the cap SHALL be allowed and recorded. The default timeout for prompt hooks SHALL be higher than the general default to accommodate model latency. Audit records for prompt evaluations SHALL record token usage.

#### Scenario: Free-text evaluator output is not trusted
- **WHEN** the evaluator model responds with prose instead of invoking the structured decision tool
- **THEN** the evaluation is treated as a failure and the hook's failure policy applies — the prose is never interpreted as a verdict

#### Scenario: Invocation cap bounds cost
- **WHEN** a prompt hook matches more tool calls in one run than its per-run cap
- **THEN** calls beyond the cap proceed without evaluation, each recorded as capped in the audit log

### Requirement: Script (Goja) handler
The `script` handler SHALL execute author-written JavaScript in-process via an embedded pure-Go interpreter. The script SHALL be stored as a string inside the hook config — no interpreter selection, no managed files, no environment rows, no working directory. It SHALL run as a function of the event (`input`) with NO host capabilities bound: no network, filesystem, environment, or process access. A returned decision object with a block decision and reason SHALL block exactly like the other handlers' block verdicts; any other return value SHALL mean allow; an uncaught exception SHALL be a handler failure governed by the failure policy. The budget SHALL be the hook's timeout enforced by VM interruption with a memory limit as an allocation backstop, and console output SHALL be captured for the test panel. Saving SHALL compile the script without executing it and SHALL reject a syntax error with the error's position. Script hooks SHALL NOT require a matcher that narrows selection. An operator kill switch SHALL be able to disable the handler fleet-wide.

#### Scenario: Script blocks a destructive call
- **WHEN** a `pre_tool_use` script hook returns a block decision with a reason
- **THEN** the tool call is blocked with that reason exactly as the command handler's exit-2 path

#### Scenario: The script cannot reach the host
- **WHEN** a script references network, filesystem, or environment globals
- **THEN** evaluation fails inside the VM and nothing outside it is touched

#### Scenario: An exception is a failure, not a block
- **WHEN** the script throws an uncaught error
- **THEN** the hook's failure policy applies and the audit record shows the failure, not a block

#### Scenario: Syntax error rejected at save
- **WHEN** a hook is saved whose script does not compile
- **THEN** the API returns a validation error naming the line and column of the first syntax error

### Requirement: Three-level scope
Hooks SHALL exist at three levels. Instance hooks SHALL apply to every workspace and agent, SHALL NOT be disableable or weakened by any workspace, and SHALL be deletable only by the instance administrator; they SHALL be visible read-only to workspaces. Workspace hooks SHALL apply to every agent in the workspace, gated only by their own enabled switch. Agent hooks SHALL be defined on and private to a single agent, applying to that agent automatically. A hook SHALL NOT be attachable per agent at the workspace or instance level.

#### Scenario: Workspace cannot weaken instance policy
- **WHEN** a workspace administrator lists hooks that reach their agents and one is instance-level
- **THEN** it is shown read-only, with no control to disable or exclude it

#### Scenario: Agent-level hook is private
- **WHEN** an agent defines its own hook and another agent in the same workspace lists its applicable hooks
- **THEN** the other agent does not see or execute the private hook

### Requirement: Ordering and precedence
Hooks SHALL evaluate in a fixed tier order — instance hooks, then workspace hooks, then agent hooks — and in display-list order within a tier. Ordering SHALL be controlled by repositioning hooks in the list (drag order in the UI; insertion order via the API); there SHALL be no numeric priority field. A workspace SHALL be able to place a fast local check ahead of an expensive evaluator so the evaluator does not execute for calls already blocked.

#### Scenario: First block wins by list position
- **WHEN** two hooks would both block the same tool call and the first in list order blocks
- **THEN** the second hook does not execute and the first hook's reason is the one reported

### Requirement: Instance hook definitions ship with the server
Instance-level hook definitions that ship with the product SHALL be embedded in the server binary and synchronized into persistent storage at every startup: absent definitions are created, changed definitions are updated when their version is newer, and definitions removed from the binary are deleted. The synchronization MUST be idempotent and safe under concurrent server starts, MUST never downgrade a newer stored definition, and MUST NOT touch workspace, agent, or administrator-created instance hooks. Audit records for deleted hooks SHALL be preserved. Shipped definitions MUST be self-contained (no operator-supplied secrets), and v1 SHALL ship none. A server MUST gracefully skip a stored hook it cannot interpret — unknown handler type, unknown event, or uncompilable matcher — recording the hook as errored rather than failing runs.

#### Scenario: Release updates a builtin hook
- **WHEN** a new server version ships a changed builtin hook definition and restarts
- **THEN** the stored definition reflects the new content without manual steps, and administrators observe the update in the hooks list

#### Scenario: Rolling deploy with an old binary
- **WHEN** an upgraded pod synchronizes a builtin definition that an older, still-serving pod cannot interpret
- **THEN** the older pod skips that hook with an errored status and runs continue unaffected

### Requirement: Audit and health surfacing
Every executed hook evaluation SHALL be recorded: which hook, event, decision, duration, failure detail (exit code or HTTP status, error or stderr snippet, truncated), and token usage where applicable. Records SHALL survive hook deletion with the hook's name preserved. Each hook SHALL expose a health status reflecting its most recent delivery outcome, and execution history SHALL be viewable per hook. A hook execution that blocked an action SHALL be visible in the transcript at the point it occurred.

#### Scenario: History survives deletion
- **WHEN** a hook with recorded executions is deleted and an administrator opens the execution history
- **THEN** past executions remain listed under the deleted hook's name

### Requirement: Hook test (dry run)
A hook MAY be tested with a synthetic event before or after saving. The test SHALL perform a real handler execution, report the decision, duration, and handler-specific detail (exit code for commands, HTTP status for webhooks, token usage for evaluators), and SHALL NOT write an audit record or affect any run.

#### Scenario: Test does not pollute audit
- **WHEN** a workspace administrator runs a hook test that blocks the synthetic event
- **THEN** the test result is displayed and no execution record is created

### Requirement: Hook permissions and API
Workspace hook CRUD SHALL require a `hooks.write` permission and reading hooks or their execution history SHALL require `hooks.read`; both permissions SHALL be granted to the built-in administrator roles via an idempotent backfill. Instance-level hook management (administrator-created definitions, shipped-definition visibility) SHALL live on the instance administration surface and require the corresponding instance-admin permissions. Hook resolution at run time SHALL be server-side only; the run APIs SHALL accept no hook input.

#### Scenario: Member cannot create hooks
- **WHEN** a workspace member without `hooks.write` attempts to create a hook
- **THEN** the request is rejected with an authorization error
