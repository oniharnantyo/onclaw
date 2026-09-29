# Spec Delta

## Purpose

Background execution for the agent's shell tool: long-running commands — test suites, builds, downloads — return a task id immediately so the conversation continues, with interim output pollable and cancellation available, under the same run-scoped background-task space as background delegation.

## ADDED Requirements

### Requirement: Background shell execution
When an agent has the shell tool and background shell execution is enabled for it — the reserved `background_shell` name is default-on under the agent tool denylist (refactor-agent-tools-denylist D3), so it resolves on unless the agent's `disabled_tools` denies the name — the shell tool SHALL accept a `run_in_background` argument. A background launch SHALL return promptly with the task id, the running status, and the output file location while the command keeps executing in the shell environment. Background commands SHALL run with the same jail, environment, and working directory as foreground shell commands.

#### Scenario: Long command backgrounded
- **WHEN** the model calls the shell tool with run_in_background set and a long-running command
- **THEN** the tool result returns promptly with the task id and output location, the conversation continues without waiting, and the command runs to completion in the background

#### Scenario: Same jail as foreground
- **WHEN** a background command executes
- **THEN** it runs inside the same jailed workspace, with the same working directory and environment, as an equivalent foreground command

### Requirement: Foreground shell unchanged
Enabling background shell execution SHALL NOT change foreground shell behavior: no foreground timer is introduced, a foreground command runs until it returns or the run is cancelled, and no command is ever moved to the background automatically. Backgrounding SHALL be explicit only.

#### Scenario: Foreground command runs to completion
- **WHEN** the model calls the shell tool without run_in_background while background shell is enabled
- **THEN** the call behaves exactly as it does without the capability — the result returns when the command finishes, however long that takes

#### Scenario: No automatic backgrounding
- **WHEN** a foreground command exceeds any duration
- **THEN** it is never detached to the background on its own; only an explicit run_in_background call starts a background task

### Requirement: Shared background-task space
Shell background tasks SHALL share the run's background-task space with background delegation tasks: the same `task_output` tool inspects interim command output and the same `task_stop` tool cancels a running command, addressing both task kinds by id. When a background command completes, fails, or is canceled, the parent session SHALL receive a completion transcript event naming the task, its outcome, and where the full output lives.

#### Scenario: Control tools address shell tasks
- **WHEN** the model calls task_output or task_stop with a background shell task's id
- **THEN** the result reflects that command's newest output or its cancellation, exactly as for a delegation task

#### Scenario: Command completion surfaces in the parent session
- **WHEN** a background command reaches a terminal status
- **THEN** the parent session gains a completion transcript event identifying the task, its outcome, and its output location, so the next turn's context carries the outcome

### Requirement: Process-local lifetime honesty
Background shell tasks are children of the server process: a task does not survive the parent run ending or the server restarting, and the launch result SHALL say so honestly. Background shell execution SHALL be available only where the shell capability itself is enabled — it never widens which agents can execute commands.

#### Scenario: Restart loses commands
- **WHEN** the server restarts while background commands are in flight
- **THEN** those tasks are gone; no ghost tasks survive, and nothing claims they will

#### Scenario: Opt-in does not grant shell
- **WHEN** background shell execution is asked to wire for an agent whose turn carries no shell tool
- **THEN** the lane resolves off rather than provisioning shell access on its own, and a direct composition supplying the lane without a shell fails fast — background execution requires the shell capability
