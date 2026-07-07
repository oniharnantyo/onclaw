## Context

**Current State:**
The `HistoryMiddleware` in `internal/agent/middlewares/history_middleware.go` only extracts `response_id` from provider-specific extensions:
- `OpenAIExtension.ID` for OpenAI-compatible providers
- `GeminiExtension.ID` for Google Gemini

**Problem:**
When using other providers (AWS Bedrock, custom models, Ollama), these extensions are `nil`, resulting in empty `response_id` values in `conversation_messages` table. This breaks:
- Response chaining (`previous_response_id` references)
- Langfuse trace correlation
- Conversation replay and debugging

**Constraints:**
- Must not break existing OpenAI/Gemini behavior
- No new external dependencies
- Minimal code changes to middleware

## Goals / Non-Goals

**Goals:**
- Ensure `response_id` is populated for all LLM providers
- Use Eino framework's `_eino_msg_id` as universal fallback
- Maintain backward compatibility with existing providers
- Add validation and error logging for missing IDs

**Non-Goals:**
- Changing database schema (`response_id` field remains string)
- Modifying conversation storage API
- Adding new telemetry systems

## Decisions

### Decision 1: Use `_eino_msg_id` as universal fallback
**Rationale:**
- Eino framework automatically populates `_eino_msg_id` in `AgenticMessage.Extra` for all providers
- UUID format guarantees uniqueness across providers
- Already available in message metadata — no additional API calls

**Alternatives Considered:**
- Generate UUID in middleware → Rejected: Adds complexity, duplicates Eino's existing ID
- Use sequence number as ID → Rejected: Not globally unique, breaks on replay
- Make `response_id` nullable → Rejected: Breaks response chaining contract

### Decision 2: Fallback chain priority
**Extraction Order:**
1. Provider-specific extensions (`OpenAIExtension.ID`, `GeminiExtension.ID`)
2. Eino metadata (`_eino_msg_id` from `Extra`)
3. Accept empty (graceful degradation)

**Rationale:**
- Preserves existing behavior for OpenAI/Gemini
- Provides universal fallback for other providers
- Handles edge cases where neither is available

### Decision 3: Type-safe extraction from Extra map
**Approach:**
```go
if responseID == "" && finalAssistantMsg.Extra != nil {
    if einoID, ok := finalAssistantMsg.Extra["_eino_msg_id"].(string); ok && einoID != "" {
        responseID = einoID
    }
}
```

**Rationale:**
- Type assertion ensures runtime safety
- Empty check prevents accepting whitespace-only IDs
- Minimal code addition (~5 lines)

## Risks / Trade-offs

**Risk:** `_eino_msg_id` field name or location changes in future Eino versions
- **Mitigation:** Field name is part of Eino's public contract; unlikely to change without notice. Consider adding integration test.

**Trade-off:** Accepting empty `response_id` when no ID available
- **Reasoning:** Better to persist conversation turn than fail entirely. Empty ID degrades gracefully (chaining breaks for that turn only).

**Risk:** Performance overhead of map lookup
- **Impact:** Negligible — single map lookup per conversation turn (<1μs)

## Migration Plan

**Deployment Steps:**
1. Deploy middleware change to production
2. Verify new conversation turns have non-empty `response_id` via monitoring
3. No database migration needed (existing empty IDs remain)

**Rollback Strategy:**
- Revert middleware code change
- New turns revert to empty `response_id` for non-OpenAI/Gemini providers
- No data loss — empty IDs are already present in production

## Open Questions

**Q:** Should we backfill existing empty `response_id` values?
**A:** No — would require re-running Eino framework to regenerate `_eino_msg_id`, not feasible for persisted turns. Empty IDs in historical data are acceptable.

**Q:** Should we emit a metric when fallback is used?
**A:** Nice-to-have — could add Prometheus counter for `eino_msg_id_fallback_used` to monitor adoption. Not required for initial implementation.