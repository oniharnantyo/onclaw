# Spec Delta

## Purpose

Execute agent conversations on the eino runtime: select a configured provider and model, stream each turn's output incrementally, account for context-window usage, and compact long sessions so chats can continue indefinitely.

## ADDED Requirements

### Requirement: Provider registry

The runtime SHALL execute turns through providers configured in settings (each with its API credential and available models). An agent MUST reference a configured provider and model to run. Selecting an unconfigured or unreachable provider MUST surface a clear error to the chat UI instead of silently failing.

#### Scenario: Turn on a configured provider

- **WHEN** the user sends a message to an agent whose provider and model are configured and reachable
- **THEN** the agent responds using that model

#### Scenario: Unreachable provider

- **WHEN** the configured provider cannot be reached
- **THEN** the session records an error event and the chat UI shows the failure for that turn

### Requirement: Streamed turns

A turn's output SHALL be delivered incrementally to the UI as it is produced — assistant text deltas, reasoning, and tool activity — rather than appearing only at completion. The final transcript MUST be persisted regardless of whether the client stays connected.

#### Scenario: Token-by-token rendering

- **WHEN** an agent is producing a response
- **THEN** the chat view renders partial output as it arrives

#### Scenario: Disconnected client does not lose the turn

- **WHEN** the web client disconnects mid-turn and reconnects after completion
- **THEN** the completed turn is present in the session transcript

### Requirement: Multi-turn context

Within a session, each turn SHALL include the session's prior context so the agent can refer to earlier messages. Context MUST persist across app restarts.

#### Scenario: Follow-up question

- **WHEN** the user asks a follow-up that references an earlier message in the same session
- **THEN** the agent's answer is consistent with that earlier message

### Requirement: Context-window accounting

The runtime SHALL track token usage for the session's current context and expose it to the UI (used tokens relative to the model's context limit) so the chat view can display context fill state.

#### Scenario: Context ring reflects usage

- **WHEN** turns accumulate in a session
- **THEN** the context indicator in the chat view shows growing usage up to the model's limit

### Requirement: Compaction

When a session's context approaches the model's limit, the runtime SHALL compact the session: summarize older history into a checkpoint, persist it, and continue the session within the limit. The user MUST NOT lose the visible transcript; compaction affects the model's working context, not the stored history.

#### Scenario: Long session continues

- **WHEN** a session's context exceeds the compaction threshold
- **THEN** the runtime compacts, the transcript in the UI shows a compaction marker, and the next turn succeeds within the context limit
