# agent-hooks Specification

## Purpose

Fire user-defined hooks during agent turns — on five lifecycle events, via command or script handlers — with matchers, an allow/block decision contract, global and per-agent resolution, and an execution audit trail.

## Requirements

### Requirement: Hook events

Hooks SHALL be dispatched at five points in a turn: run started, user prompt submit, before each tool call, after each tool call, and run finished. A failing hook MUST NOT crash the turn; its outcome follows the hook's failure policy and is recorded.

#### Scenario: Pre-tool hook fires per call

- **WHEN** a turn makes three tool calls and a pre-tool-use hook is bound
- **THEN** the hook is evaluated before each of the three calls

#### Scenario: Failing hook does not break the turn

- **WHEN** a hook's handler times out or errors
- **THEN** the turn continues and the failure is recorded against that hook execution

### Requirement: Handler types

Hooks SHALL support two handler types: command (a subprocess receiving the event as JSON on stdin, with a bounded timeout) and script (sandboxed in-process JavaScript with no host bindings beyond a console). Other handler types are out of scope.

#### Scenario: Command hook receives the event

- **WHEN** a command hook fires
- **THEN** the handler process receives the event JSON on stdin within the timeout

#### Scenario: Script hook runs sandboxed

- **WHEN** a script hook fires
- **THEN** the script executes in-process without host access beyond the event it is given

### Requirement: Decision contract

A hook MAY return a decision of allow or block with a reason. A block on user prompt submit SHALL prevent the prompt from starting a turn; a block before a tool call SHALL prevent that tool from executing. The block reason SHALL be visible in the transcript. Hooks that return no decision are informational and never block.

#### Scenario: Blocked tool never executes

- **WHEN** a pre-tool-use hook returns block for a tool call
- **THEN** the tool does not run and the transcript records the blocked call with the hook's reason

#### Scenario: Blocked prompt does not start a turn

- **WHEN** a user-prompt-submit hook returns block
- **THEN** no model call is made for that prompt and the reason is shown

### Requirement: Resolution order

An agent's turn SHALL evaluate that agent's assigned hooks first, then the global hooks, in configured order within each tier.

#### Scenario: Agent hooks run before global hooks

- **WHEN** an agent has an assigned hook and a global hook exists for the same event
- **THEN** the agent's hook is evaluated first on its turns

### Requirement: Matchers

A hook MAY restrict when it fires with a matcher (for tool events, by tool name pattern). Non-matching events SHALL skip the hook without executing it.

#### Scenario: Matcher limits to one tool

- **WHEN** a pre-tool-use hook matches only the `execute` tool
- **THEN** calls to other tools do not invoke the hook

### Requirement: Execution audit

Every hook evaluation SHALL be recorded — hook, event, outcome (allowed, blocked, error), and duration — and the records SHALL be viewable.

#### Scenario: Audits accumulate

- **WHEN** hooks fire across turns
- **THEN** each evaluation appears in the audit view with its outcome and duration
