## ADDED Requirements

### Requirement: Response ID must never be empty
The system SHALL guarantee that every persisted conversation turn has a non-empty `response_id` field when possible.

#### Scenario: New conversation turn with provider response ID
- **WHEN** a conversation turn is persisted with provider-supplied response ID
- **THEN** `response_id` field MUST contain a non-empty string
- **AND** `previous_response_id` references the prior turn's `response_id`

#### Scenario: Fallback to Eino message ID
- **WHEN** provider does not supply a response ID
- **AND** Eino framework has populated `_eino_msg_id` in message metadata
- **THEN** system MUST use `_eino_msg_id` as `response_id`
- **AND** `response_id` field MUST NOT be empty

#### Scenario: Response chaining across providers
- **WHEN** conversation spans multiple providers (e.g., OpenAI → Bedrock)
- **THEN** each turn MUST have a valid `response_id` regardless of provider
- **AND** `previous_response_id` MUST correctly reference the immediate predecessor

#### Scenario: Graceful degradation when no ID available
- **WHEN** neither provider nor Eino metadata provides a response ID
- **THEN** system MAY persist with empty `response_id`
- **AND** system MUST log a warning about missing response ID