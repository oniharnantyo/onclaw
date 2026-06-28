# providers (delta)

## Purpose

Delta for the existing `providers` capability: the OpenAI-compatible provider kinds now build
real streaming ChatModels rather than no-op stubs.

## ADDED Requirements

### Requirement: OpenAI-compatible kinds build a real streaming ChatModel

The system SHALL resolve profiles of kind `openai`, `openai-compatible`, and `ollama` to a
real `model.ChatModel` that performs live, streaming inference against the profile's
`api_base`/model, constructed via eino-ext or an equivalent hand-rolled client. These kinds
SHALL NOT resolve to a no-op stub. A disabled profile SHALL NOT be built.

#### Scenario: An OpenAI-compatible profile produces live output

- **WHEN** a profile of kind `openai-compatible` with a valid base URL, model, and key is built and used for a turn
- **THEN** the model streams real completions over the network and is not a no-op stub

#### Scenario: Ollama uses the OpenAI-compatible path with no key

- **WHEN** a keyless `ollama` profile with a base URL is built
- **THEN** it resolves to a real ChatModel via the OpenAI-compatible path without requiring an API key

#### Scenario: A disabled profile is not built

- **WHEN** the selected profile has `enabled` set false
- **THEN** building it fails with a clear error rather than producing a model

### Requirement: A normalized reasoning-effort value is mapped to the provider's native field

The OpenAI-compatible adapter SHALL read a normalized `reasoning_effort` (`low`, `medium`,
`high`, or unset) from the effective profile's `settings` and SHALL map it to the provider's
native request field. A value the provider does not support SHALL be ignored (no effort sent)
rather than causing an error. This is how an agent's or `--reasoning` selection takes effect
(cross-ref `agent-profiles`).

#### Scenario: A high effort is sent to the provider

- **WHEN** the effective profile carries `reasoning_effort: high` and the provider supports it
- **THEN** the outbound request carries the provider's native high-effort field

#### Scenario: An unsupported effort is dropped

- **WHEN** the effective profile carries `reasoning_effort: high` but the provider kind does not support it
- **THEN** the request is sent with no effort field and does not error
