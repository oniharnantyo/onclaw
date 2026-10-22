# Spec Delta

## Purpose

Let agents delegate work to a general-purpose subagent through the `agent` tool, optionally in a background lane with task control tools and materialized child transcripts.

## ADDED Requirements

### Requirement: Delegation via the agent tool

An agent with subagents enabled SHALL have an `agent` tool that delegates a task to a general-purpose subagent: a clone of the parent's persona documents, model, and tool surface with a fresh context, which cannot itself delegate. The subagent's final answer SHALL return to the parent as the tool result.

#### Scenario: Delegation returns a result

- **WHEN** the parent delegates a task to the subagent
- **THEN** the subagent runs to completion and its answer returns as the agent tool's result

#### Scenario: No recursion

- **WHEN** the subagent's tool surface is resolved
- **THEN** it has no `agent` tool of its own

### Requirement: Subagent toggle

Users SHALL be able to enable or disable subagents per agent; a disabled agent's turns are offered no `agent` tool.

#### Scenario: Disabled agent cannot delegate

- **WHEN** subagents are disabled for an agent
- **THEN** its turns are offered no agent tool

### Requirement: Background lane

When subagents are enabled, the `agent` tool SHALL support running a delegation in the background: the parent can continue its own work and later collect output via `task_output` or cancel via `task_stop`. Task identifiers SHALL be unique within the run.

#### Scenario: Parent continues while a task runs

- **WHEN** a delegation runs in the background
- **THEN** the parent turn proceeds and the task completes independently

#### Scenario: Output is collected

- **WHEN** the parent calls task_output for a finished task
- **THEN** the subagent's result is returned

#### Scenario: Task is cancelled

- **WHEN** the parent calls task_stop for a running task
- **THEN** the subagent run is cancelled and reported as stopped

### Requirement: Child transcripts

Each subagent run SHALL materialize a transcript under the parent agent's workspace `.tasks/` directory, readable through the filesystem tools.

#### Scenario: Transcript is inspectable

- **WHEN** a subagent run completes
- **THEN** its transcript is present under the workspace `.tasks/` directory and readable by the parent's file tools

### Requirement: Task completion surfacing

Background task completion SHALL be recorded on the parent session's transcript so it is visible in chat and survives restarts.

#### Scenario: Completion is visible in chat

- **WHEN** a background task finishes
- **THEN** the parent session transcript records the completion and the chat view shows it
