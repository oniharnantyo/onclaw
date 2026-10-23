# Spec Delta

## MODIFIED Requirements

### Requirement: Provider registry

The runtime SHALL execute turns through providers configured in settings (each with its API credential and available models). An agent MUST reference a configured provider and model to run. Selecting an unconfigured or unreachable provider MUST surface a clear error to the chat UI instead of silently failing.

Each turn SHALL execute on the provider's native agentic message protocol — content blocks covering reasoning, multimodal input, and tool calls — rather than a legacy flat message schema. A provider whose endpoint does not implement the protocol its family requires SHALL be surfaced as unavailable for the turn with a clear reason, not attempted and failed mid-turn.

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

#### Scenario: Endpoint without the required protocol is unavailable

- **WHEN** an agent's configured provider endpoint does not implement the agentic protocol its provider family requires
- **THEN** the turn fails with a clear unavailability reason before any model request is made
