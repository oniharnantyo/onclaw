package middlewares

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type contextKey string

const (
	cursorKey            = contextKey("onclaw_history_cursor")
	persistedKey         = "_onclaw_persisted"
	prevResponseIDCtxKey = contextKey("onclaw_prev_response_id")
)

var uuidRegex = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// WithPreviousResponseID attaches a client-supplied previous response ID to the context.
func WithPreviousResponseID(ctx context.Context, prevID string) context.Context {
	return context.WithValue(ctx, prevResponseIDCtxKey, prevID)
}

// GetPreviousResponseID retrieves the client-supplied previous response ID from the context.
func GetPreviousResponseID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(prevResponseIDCtxKey).(string)
	return val, ok
}

// RunCursor tracks the max sequence number within a single run.
type RunCursor struct {
	MaxSeq int64
}

// HistoryMiddleware embeds Eino's base middleware and persists/replays conversation history.
type HistoryMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	Store              store.ConversationStore
	ConversationID     int64
	Model              string
	previousResponseID string
	bufferedMessages   []*schema.AgenticMessage
	lastTurnMeta       *store.TurnMeta
	lock               sync.Mutex
}

// NewHistoryMiddleware creates a new HistoryMiddleware.
func NewHistoryMiddleware(s store.ConversationStore, convID int64, model string) *HistoryMiddleware {
	return &HistoryMiddleware{
		Store:          s,
		ConversationID: convID,
		Model:          model,
	}
}

func (h *HistoryMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.AgenticMessage]) (context.Context, *adk.ChatModelAgentContext[*schema.AgenticMessage], error) {
	// 1. Load History
	summaryRow, tailRows, err := h.Store.LoadHistory(ctx, h.ConversationID)
	if err != nil {
		return ctx, nil, fmt.Errorf("load history: %w", err)
	}

	var historyMessages []*schema.AgenticMessage

	unmarshalTurn := func(row *store.TurnRow) ([]*schema.AgenticMessage, error) {
		var msgs []*schema.AgenticMessage
		if err := json.Unmarshal([]byte(row.Message), &msgs); err != nil {
			return nil, fmt.Errorf("unmarshal turn messages: %w", err)
		}
		for _, msg := range msgs {
			if msg.Extra == nil {
				msg.Extra = make(map[string]interface{})
			}
			msg.Extra[persistedKey] = true
			msg.Extra["_onclaw_seq"] = row.SequenceNum
		}
		return msgs, nil
	}

	if summaryRow != nil {
		sMsgs, err := unmarshalTurn(summaryRow)
		if err != nil {
			return ctx, nil, err
		}
		historyMessages = append(historyMessages, sMsgs...)
	}

	for _, row := range tailRows {
		tMsgs, err := unmarshalTurn(row)
		if err != nil {
			return ctx, nil, err
		}
		historyMessages = append(historyMessages, tMsgs...)
	}

	// Track the previous response ID from the last loaded turn
	var prevResponseID string
	var maxSeq int64
	if len(tailRows) > 0 {
		prevResponseID = tailRows[len(tailRows)-1].ResponseID
		maxSeq = tailRows[len(tailRows)-1].SequenceNum
	} else if summaryRow != nil {
		prevResponseID = summaryRow.ResponseID
		maxSeq = summaryRow.SequenceNum
	}

	if clientPrevID, ok := GetPreviousResponseID(ctx); ok && clientPrevID != "" {
		prevResponseID = clientPrevID
	}
	h.previousResponseID = prevResponseID

	// Inject history before the user input messages
	originalMessages := runCtx.AgentInput.Messages
	runCtx.AgentInput.Messages = append(historyMessages, originalMessages...)

	// Buffer the new user message (no eager write)
	h.bufferedMessages = nil
	for _, msg := range originalMessages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		if !IsPersisted(msg) {
			h.bufferedMessages = append(h.bufferedMessages, msg)
		}
	}

	// Initialize the run cursor in context
	cursor := &RunCursor{MaxSeq: maxSeq}
	ctx = context.WithValue(ctx, cursorKey, cursor)

	return ctx, runCtx, nil
}

func (h *HistoryMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], modelCtx *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	h.accumulateNewMessages(state.Messages)
	return ctx, state, nil
}

func (h *HistoryMiddleware) AfterAgent(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage]) (context.Context, error) {
	h.accumulateNewMessages(state.Messages)

	if len(h.bufferedMessages) == 0 {
		return ctx, nil
	}

	// 1. Extract question and answer
	question, answer := extractQuestionAndAnswer(h.bufferedMessages)

	// 2. Extract token usage and response_id from the final assistant message
	var prompt, completion, total int64
	var responseID string
	var finalAssistantMsg *schema.AgenticMessage

	// Find the final assistant message in the turn
	for i := len(h.bufferedMessages) - 1; i >= 0; i-- {
		msg := h.bufferedMessages[i]
		if msg.Role == schema.AgenticRoleTypeAssistant {
			finalAssistantMsg = msg
			break
		}
	}

	if finalAssistantMsg != nil && finalAssistantMsg.ResponseMeta != nil {
		if finalAssistantMsg.ResponseMeta.TokenUsage != nil {
			prompt = int64(finalAssistantMsg.ResponseMeta.TokenUsage.PromptTokens)
			completion = int64(finalAssistantMsg.ResponseMeta.TokenUsage.CompletionTokens)
			total = int64(finalAssistantMsg.ResponseMeta.TokenUsage.TotalTokens)
		}
		if finalAssistantMsg.ResponseMeta.OpenAIExtension != nil {
			responseID = finalAssistantMsg.ResponseMeta.OpenAIExtension.ID
		} else if finalAssistantMsg.ResponseMeta.GeminiExtension != nil {
			responseID = finalAssistantMsg.ResponseMeta.GeminiExtension.ID
		}
	}

	// Fallback chain priority:
	// 1. Provider-specific extensions (OpenAIExtension, GeminiExtension)
	// 2. Universal Eino framework message ID fallback (_eino_msg_id from Extra)
	// 3. Graceful degradation (empty ID with warning log)
	var isEinoID bool
	if responseID == "" && finalAssistantMsg != nil && finalAssistantMsg.Extra != nil {
		if einoID, ok := finalAssistantMsg.Extra["_eino_msg_id"].(string); ok && einoID != "" {
			responseID = einoID
			isEinoID = true
		}
	}

	// Validate Eino fallback ID format (must be a valid UUID)
	if responseID != "" && isEinoID {
		if !uuidRegex.MatchString(responseID) {
			log.Printf("HistoryMiddleware: invalid response ID %q (expected UUID), using empty fallback", responseID)
			responseID = ""
		}
	}

	if responseID == "" {
		log.Printf("HistoryMiddleware: missing response ID for conversation %d, using empty fallback", h.ConversationID)
	}

	// 3. Marshal redacted buffered messages
	var redactedMessages []*schema.AgenticMessage
	for _, msg := range h.bufferedMessages {
		redactedMsg := tools.RedactAgenticMessage(msg)
		if redactedMsg.Extra == nil {
			redactedMsg.Extra = make(map[string]interface{})
		}
		redactedMsg.Extra[persistedKey] = true
		redactedMessages = append(redactedMessages, redactedMsg)
	}

	msgArrayJSONBytes, err := json.Marshal(redactedMessages)
	if err != nil {
		return ctx, fmt.Errorf("marshal turn messages to JSON: %w", err)
	}
	msgArrayJSON := string(msgArrayJSONBytes)

	// 4. Commit turn row
	seq, err := h.Store.AppendTurn(
		ctx,
		h.ConversationID,
		msgArrayJSON,
		responseID,
		h.previousResponseID,
		h.Model,
		prompt,
		completion,
		total,
		question,
		answer,
	)
	if err != nil {
		return ctx, fmt.Errorf("append turn to store: %w", err)
	}

	// 5. Flag buffered messages persisted and set sequence num
	for _, msg := range h.bufferedMessages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		msg.Extra[persistedKey] = true
		msg.Extra["_onclaw_seq"] = seq
	}

		// 6. Record lastTurnMeta
	h.lock.Lock()
	h.lastTurnMeta = &store.TurnMeta{
		ConversationID:     h.ConversationID,
		SequenceNum:        seq,
		ResponseID:         responseID,
		PreviousResponseID: h.previousResponseID,
		Model:              h.Model,
		Tokens:             total,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
	}
	h.lock.Unlock()

	// Clear buffer
	h.bufferedMessages = nil

	return ctx, nil
}

func (h *HistoryMiddleware) LastTurnMeta() *store.TurnMeta {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.lastTurnMeta
}

func (h *HistoryMiddleware) accumulateNewMessages(stateMessages []*schema.AgenticMessage) {
	if len(stateMessages) == 0 {
		return
	}
	bufferedSet := make(map[*schema.AgenticMessage]struct{}, len(h.bufferedMessages))
	for _, bm := range h.bufferedMessages {
		bufferedSet[bm] = struct{}{}
	}
	for _, msg := range stateMessages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		if !IsPersisted(msg) {
			if _, ok := bufferedSet[msg]; !ok {
				h.bufferedMessages = append(h.bufferedMessages, msg)
				bufferedSet[msg] = struct{}{}
			}
		}
	}
}

// IsPersisted checks if a message has been saved to the store.
func IsPersisted(msg *schema.AgenticMessage) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	val, ok := msg.Extra[persistedKey]
	if !ok {
		return false
	}
	b, ok := val.(bool)
	return ok && b
}

func getAgenticMessageText(msg *schema.AgenticMessage) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}
		if block.UserInputText != nil {
			sb.WriteString(block.UserInputText.Text)
		} else if block.AssistantGenText != nil {
			sb.WriteString(block.AssistantGenText.Text)
		} else if block.FunctionToolResult != nil {
			for _, cb := range block.FunctionToolResult.Content {
				if cb != nil && cb.Text != nil {
					sb.WriteString(cb.Text.Text)
				}
			}
		}
	}
	return sb.String()
}

func extractQuestionAndAnswer(messages []*schema.AgenticMessage) (string, string) {
	var question, answer string
	for _, msg := range messages {
		if msg.Role == schema.AgenticRoleTypeUser {
			if question == "" {
				question = getAgenticMessageText(msg)
			}
		} else if msg.Role == schema.AgenticRoleTypeAssistant {
			answer = getAgenticMessageText(msg)
		}
	}
	return question, answer
}
