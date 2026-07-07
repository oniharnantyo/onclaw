## Why

Conversation history tracking is broken when using non-OpenAI/Gemini LLM providers (e.g., AWS Bedrock, custom models). The `response_id` field in `conversation_messages` table remains empty, breaking response chaining and traceability. This affects production users who need reliable conversation tracking regardless of provider choice.

## What Changes

- **Extract fallback response ID from Eino trace metadata**: Use `_eino_msg_id` from `AgenticMessage.Extra` when provider-specific extensions (`OpenAIExtension.ID`, `GeminiExtension.ID`) are unavailable
- **Update history middleware**: Modify `HistoryMiddleware.AfterAgent` to check `_eino_msg_id` in message `Extra` fields before accepting empty `response_id`
- **Ensure all conversation turns have valid response IDs**: Guarantee `response_id` field is populated for every persisted conversation message, enabling proper response chaining (`previous_response_id` references)

## Capabilities

### New Capabilities
- `provider-agnostic-response-id`: Provider-agnostic response ID extraction that ensures reliable conversation tracking across all LLM providers

### Modified Capabilities
- `conversation-history`: Enhanced to guarantee non-empty response_id field for all providers (requirement change: response_id must never be empty)

## Impact

**Affected code:**
- `internal/agent/middlewares/history_middleware.go` — AfterAgent method (response_id extraction logic)
- `internal/store/sqlite/conversation.go` — AppendTurn method (no changes, but will receive non-empty response_id)

**Affected systems:**
- Conversation history tracking and replay
- Response chaining via `previous_response_id` references
- Langfuse observability traces (response_id correlation)

**Dependencies:**
- No new dependencies
- Uses existing Eino framework `_eino_msg_id` field (already populated)