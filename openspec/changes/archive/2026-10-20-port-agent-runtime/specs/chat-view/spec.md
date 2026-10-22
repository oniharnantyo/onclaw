# Spec Delta

## MODIFIED Requirements

### Requirement: Chat transcript rendering

The chat view SHALL render user messages, assistant messages, reasoning bubbles, tool-call cards with live state — running while executing, then completed with the executed result or failed with the error — and compaction markers in chronological order. Streaming output MUST be appended live during a turn.

#### Scenario: Tool call visible in transcript

- **WHEN** an agent turn includes tool activity
- **THEN** the transcript shows a tool-call card with the tool name and its result inline in message order

#### Scenario: Running state while executing

- **WHEN** a tool call is executing
- **THEN** its card shows the running state until the result arrives

#### Scenario: Failed tool shows the error

- **WHEN** a tool invocation fails
- **THEN** the card shows the failed state with the error

#### Scenario: Live streaming in transcript

- **WHEN** an agent is responding
- **THEN** partial text appears in the transcript as it streams

## ADDED Requirements

### Requirement: Background task progress

The chat view SHALL render background task activity as it happens: a task card while a task is running, its completion or cancellation, and access to the task's output once finished.

#### Scenario: Task lifecycle is visible

- **WHEN** a background task starts, runs, and finishes during a session
- **THEN** the transcript shows the running card and then the completion, and the finished task's output can be viewed
