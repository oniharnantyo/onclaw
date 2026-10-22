# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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