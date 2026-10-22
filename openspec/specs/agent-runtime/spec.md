# agent-runtime Specification

## Purpose

Execute agent conversations on the eino runtime: select a configured provider and model, stream each turn's output incrementally, account for context-window usage, and compact long sessions so chats can continue indefinitely.

## Requirements

### Requirement: Provider registry

The runtime SHALL execute turns through providers configured in settings (each with its API credential and available models). An agent MUST reference a configured provider and model to run. Selecting an unconfigured or unreachable provider MUST surface a clear error to the chat UI instead of silently failing.

Each turn SHALL resolve its effective model from the session's model override when one is set, otherwise the agent's configured model, and SHALL apply the session's effective reasoning effort — session override, else agent default, else none — through the provider's native reasoning parameter whenever the model supports one. A model override MUST be rejected with a clear per-turn error when the provider exposes a model listing that does not include it (providers that allow free-form ids may accept unknown models). An effort MUST be rejected with a clear per-turn error when the selected model's capability set does not include it, rather than being silently dropped or sent anyway.

#### Scenario: Turn on a configured provider

- **WHEN** the user sends a message to an agent whose provider and model are configured and reachable
- **THEN** the agent responds using that model

#### Scenario: Unreachable provider

- **WHEN** the configured provider cannot be reached
- **THEN** the session records an error event and the chat UI shows the failure for that turn

#### Scenario: Session model override wins

- **WHEN** a session has a model override set from the composer while its agent is configured with a different model
- **THEN** the turn executes on the session's overridden model

#### Scenario: Session effort reaches the provider

- **WHEN** a session has an effort set for a model that supports reasoning effort
- **THEN** the provider request carries that effort as its native reasoning parameter

#### Scenario: Unsupported effort rejected

- **WHEN** a session's effort is not in the selected model's supported set
- **THEN** the turn fails with a clear error event in the transcript instead of running at some other effort

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

### Requirement: Turn cancellation

The runtime SHALL support cancelling an in-flight turn for a session through a bound call. Cancellation SHALL stop further model and tool activity for that turn, persist a terminal cancelled outcome with the partial output retained in the transcript, and release the session so the next queued or new message dispatches normally. Cancelling a turn paused on a pending approval MUST discard the run and clear the pending approval. Invoking cancellation when no turn is in flight MUST be safe and MUST NOT disturb session state.

#### Scenario: Cancel mid-stream

- **WHEN** the user cancels a turn while text is streaming
- **THEN** streaming stops, the partial text remains in the transcript with a terminal cancelled outcome recorded, and the session accepts a new send

#### Scenario: Cancel releases the queue

- **WHEN** messages are queued behind the cancelled turn
- **THEN** the next queued message dispatches automatically after the cancellation

#### Scenario: Cancel with no active turn

- **WHEN** cancellation is invoked while the session has no turn in flight
- **THEN** the call completes safely without altering the transcript or session state

### Requirement: Multi-turn context

Within a session, each turn SHALL include the session's prior context so the agent can refer to earlier messages. Context MUST persist across app restarts.

#### Scenario: Follow-up question

- **WHEN** the user asks a follow-up that references an earlier message in the same session
- **THEN** the agent's answer is consistent with that earlier message

### Requirement: Context-window accounting

The runtime SHALL track token usage for the session's current context and expose it to the UI — used tokens relative to the model's context limit, plus the last completed turn's input and output token counts — so the chat view can display context fill state and the last turn's cost.

#### Scenario: Context ring reflects usage

- **WHEN** turns accumulate in a session
- **THEN** the context indicator in the chat view shows growing usage up to the model's limit

#### Scenario: Last-turn usage exposed

- **WHEN** a turn completes
- **THEN** the exposed context usage includes that turn's input and output token counts alongside the used total and the limit

### Requirement: Compaction

When a session's context approaches the model's limit, the runtime SHALL compact the session: summarize older history into a checkpoint, persist it, and continue the session within the limit. The user MUST NOT lose the visible transcript; compaction affects the model's working context, not the stored history.

#### Scenario: Long session continues

- **WHEN** a session's context exceeds the compaction threshold
- **THEN** the runtime compacts, the transcript in the UI shows a compaction marker, and the next turn succeeds within the context limit
