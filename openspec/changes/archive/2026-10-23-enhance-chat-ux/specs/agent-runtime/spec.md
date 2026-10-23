# Spec Delta

## MODIFIED Requirements

### Requirement: Streamed turns

A turn's output SHALL be delivered incrementally to the UI as it is produced — assistant text deltas, reasoning, and tool activity — rather than appearing only at completion. The final transcript MUST be persisted regardless of whether the client stays connected.

Each tool activity's terminal record SHALL carry the call's wall-clock duration, measured from the moment the call was announced as running to the moment its result landed, so a reloaded transcript shows the same latency the live view did. Records persisted before durations were recorded carry none, and the runtime MUST NOT fabricate one for them.

#### Scenario: Token-by-token rendering

- **WHEN** an agent is producing a response
- **THEN** the chat view renders partial output as it arrives

#### Scenario: Disconnected client does not lose the turn

- **WHEN** the web client disconnects mid-turn and reconnects after completion
- **THEN** the completed turn is present in the session transcript

#### Scenario: Tool duration persists

- **WHEN** a tool call completes and the session is later reloaded
- **THEN** the call's terminal record carries the duration observed by the runtime and the reloaded card shows the same latency as the live card did

### Requirement: Context-window accounting

The runtime SHALL track token usage for the session's current context and expose it to the UI — used tokens relative to the model's context limit, plus the last completed turn's input and output token counts — so the chat view can display context fill state and the last turn's cost.

#### Scenario: Context ring reflects usage

- **WHEN** turns accumulate in a session
- **THEN** the context indicator in the chat view shows growing usage up to the model's limit

#### Scenario: Last-turn usage exposed

- **WHEN** a turn completes
- **THEN** the exposed context usage includes that turn's input and output token counts alongside the used total and the limit
