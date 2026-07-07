## 1. Core Implementation

- [x] 1.1 Add fallback extraction logic in `HistoryMiddleware.AfterAgent` method
- [x] 1.2 Add type-safe `_eino_msg_id` extraction from `AgenticMessage.Extra` map
- [x] 1.3 Add warning log for missing response ID (graceful degradation case)
- [x] 1.4 Verify code compiles and passes go vet

## 2. Testing

- [x] 2.1 Add unit test for OpenAI provider (existing behavior preserved)
- [x] 2.2 Add unit test for Gemini provider (existing behavior preserved)
- [x] 2.3 Add unit test for `_eino_msg_id` fallback extraction
- [x] 2.4 Add unit test for graceful degradation (no ID available)
- [x] 2.5 Add unit test for response chaining across providers
- [x] 2.6 Verify all tests pass (go test ./internal/agent/middlewares/...)

## 3. Verification

- [x] 3.1 Manual test: Create conversation with Bedrock provider, verify non-empty `response_id`
- [x] 3.2 Manual test: Create conversation with OpenAI provider, verify existing behavior
- [x] 3.3 Check database: Query `conversation_messages` table for non-empty response_id values
- [x] 3.4 Monitor logs: Verify warning appears only when no ID is available
- [x] 3.5 Integration test: Run full agent workflow with mixed providers

## 4. Documentation

- [x] 4.1 Update code comments in `HistoryMiddleware.AfterAgent` to explain fallback chain
- [x] 4.2 Add example to README or docs showing response ID extraction across providers