# provider-agnostic-response-id Specification

## Purpose
TBD - created by archiving change fix-empty-response-id. Update Purpose after archive.
## Requirements
### Requirement: Extract response ID from Eino metadata
The system SHALL extract response IDs from Eino framework metadata when provider-specific extensions are unavailable.

#### Scenario: OpenAI provider provides response ID
- **WHEN** OpenAI provider returns a response with `OpenAIExtension.ID` populated
- **THEN** system uses `OpenAIExtension.ID` as the `response_id`

#### Scenario: Gemini provider provides response ID
- **WHEN** Gemini provider returns a response with `GeminiExtension.ID` populated
- **THEN** system uses `GeminiExtension.ID` as the `response_id`

#### Scenario: Non-OpenAI/Gemini provider (fallback)
- **WHEN** provider does not populate `OpenAIExtension.ID` or `GeminiExtension.ID`
- **AND** AgenticMessage Extra contains `_eino_msg_id` field
- **THEN** system extracts `_eino_msg_id` from Extra and uses it as `response_id`

#### Scenario: No response ID available
- **WHEN** provider does not populate any extension ID
- **AND** `_eino_msg_id` is not present in Extra
- **THEN** system accepts empty `response_id` (graceful degradation)

### Requirement: Response ID format validation
The system SHALL validate extracted response IDs before persisting.

#### Scenario: Valid Eino message ID
- **WHEN** `_eino_msg_id` is a valid UUID string
- **THEN** system accepts and persists the ID

#### Scenario: Invalid or malformed ID
- **WHEN** extracted response ID is empty or invalid
- **THEN** system logs a warning and continues with empty `response_id`

