# agent-subagents Specification

## Purpose
Agent delegation: lets a workspace agent hand a self-contained subtask to a sub-agent — a fresh-conversation specialist — optionally in the background, with the sub-agent's execution hidden behind a single tool call on the parent transcript.

## Requirements

### Requirement: Delegation tool
An agent with the subagents capability SHALL expose one delegation tool that takes a subagent type, a self-contained prompt, and a short description. Dispatch SHALL start a fresh conversation with the selected sub-agent carrying only the prompt — never the parent's history. The delegation's tool result SHALL be the sub-agent's final message; the sub-agent's intermediate steps SHALL NOT enter the parent's context. A delegation call is an ordinary tool call: the parent's pre-tool-use policy sees it and a policy block returns the canonical block result. An unknown subagent type SHALL return a readable tool result naming the available types.

#### Scenario: Delegate to a declared type
- **WHEN** the model calls the delegation tool with an available subagent type and a prompt
- **THEN** a fresh conversation starts for the named sub-agent with the prompt as its only user input, and the tool result is the sub-agent's final message

#### Scenario: Unknown type rejected readably
- **WHEN** the model calls the delegation tool with a type that was not declared
- **THEN** the tool result is an error text naming the available types, and the run continues

#### Scenario: Policy block on delegation
- **WHEN** a pre-tool-use hook blocks the delegation tool call
- **THEN** the delegation does not start and the block result is returned as the tool result

### Requirement: Injected general-purpose subagent
Unless composition is configured to suppress it, the capability SHALL inject a `general-purpose` subagent type: a clone of the delegating agent — same instruction, model, tool surface, and capability wiring — so the parent can delegate any of its own work to a fresh context. The clone SHALL NOT carry the delegation tool.

#### Scenario: Available by default
- **WHEN** the capability is wired without suppression
- **THEN** the delegation tool offers `general-purpose` alongside any declared types, and the clone exposes the parent's tools but no delegation tool

#### Scenario: Suppressed by configuration
- **WHEN** the capability is wired with the general-purpose subagent suppressed
- **THEN** only caller-declared subagent types are offered; with none declared, composition fails fast

### Requirement: Declarable subagent types
Callers MAY supply additional subagent instances, each with a unique name and description, alongside the injected general-purpose type. The available types SHALL be advertised to the model in the delegation tool's description and a mid-conversation reminder.

#### Scenario: Supplied types advertised
- **WHEN** composition receives caller-declared subagent instances
- **THEN** the delegation tool's description and the reminder list each type's name and description

#### Scenario: Duplicate names rejected
- **WHEN** two declared subagent instances share a name
- **THEN** composition returns a descriptive error and constructs no agent

### Requirement: Delegation cannot recurse
Sub-agent compositions SHALL NOT carry the delegation tool or the background control tools, regardless of the parent's wiring. Delegation depth is therefore one.

#### Scenario: Sub-agent has no delegation tool
- **WHEN** a sub-agent runs
- **THEN** its tool surface contains no delegation tool and no task control tools

### Requirement: Child transcript isolation
Sub-agent execution events SHALL NOT persist onto the parent session's transcript and SHALL NOT be delivered onto the parent's live stream as transcript events. To the parent transcript — live and hydrated — a delegation is exactly one tool call with started/finished events carrying the delegation arguments and the sub-agent's final message as the result.

#### Scenario: Live stream shows one card
- **WHEN** a delegated sub-agent executes multiple model turns and tool calls
- **THEN** the parent's live stream carries one delegation tool-call started event and one finished event, and no sub-agent text, reasoning, or inner tool-call events

#### Scenario: Reload shows one card
- **WHEN** a transcript containing a completed delegation is reloaded from durable storage
- **THEN** the delegation renders as the same single tool call with its result, identical to what the live stream showed

### Requirement: Background delegation
When the capability is wired with background enabled, the delegation tool SHALL accept a `run_in_background` argument. A background launch SHALL return immediately with the task id and status while the sub-agent continues executing. The model SHALL be able to inspect a task's interim progress with a `task_output` tool and cancel it with a `task_stop` tool; those control tools address every background task in the run's shared task space, delegation tasks and shell tasks alike. When a background delegation completes, fails, or is canceled, the parent session SHALL receive a completion transcript event naming the task, its outcome, and where the full output lives. Background tasks are process-local: a task does not survive the parent run ending or the server restarting, and the launch result SHALL say so honestly.

#### Scenario: Background launch returns immediately
- **WHEN** the model calls the delegation tool with run_in_background set
- **THEN** the tool result returns promptly with the task id, running status, and the output location, and the sub-agent keeps executing

#### Scenario: Task output polling
- **WHEN** the model calls task_output with a launched task's id
- **THEN** the result contains the task's newest progress entries, newest first

#### Scenario: Task stop cancels
- **WHEN** the model calls task_stop with a running task's id
- **THEN** the task terminates with a canceled status and the cancellation is visible to subsequent task_output calls

#### Scenario: Completion surfaces in the parent session
- **WHEN** a background delegation reaches a terminal status
- **THEN** the parent session gains a completion transcript event identifying the task, its outcome, and its output location, so the next turn's context carries the outcome

#### Scenario: Restart loses tasks
- **WHEN** the server restarts or the parent run ends while background delegations are in flight
- **THEN** those tasks are gone; no ghost tasks survive, and nothing claims they will

### Requirement: Capability enablement
The capability SHALL attach only when the agent's effective tool selection carries the reserved subagents name. Under the agent tool denylist (refactor-agent-tools-denylist D3) the name is default-on: it carries unless the agent's `disabled_tools` denies it or a per-turn allowed-tools override replaces the surface without it. Without it, composition SHALL be unchanged — no delegation tool, no control tools, no added instruction. The reserved name SHALL NOT appear as a business tool on the composed surface.

#### Scenario: Not selected
- **WHEN** an agent whose `disabled_tools` denies the reserved subagents name runs
- **THEN** its composition and prompt carry no delegation capability

#### Scenario: Selected
- **WHEN** an agent's effective tool selection carries the reserved subagents name
- **THEN** the delegation capability is wired and the reserved name itself is not presented to the model as a callable business tool
