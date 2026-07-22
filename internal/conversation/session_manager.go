package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/tokens"
)

type contextKey string

const (
	persistWriteTimeout  = 5 * time.Second
	prevResponseIDCtxKey = contextKey("onclaw_prev_response_id")
)

// WithPreviousResponseID attaches a client-supplied previous response ID to the context.
func WithPreviousResponseID(ctx context.Context, prevID string) context.Context {
	return context.WithValue(ctx, prevResponseIDCtxKey, prevID)
}

// GetPreviousResponseID retrieves the client-supplied previous response ID from the context.
func GetPreviousResponseID(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(prevResponseIDCtxKey).(string)
	return val, ok
}

// SessionManager is the business-layer owner of conversation history.
// It manages history loading and turn persistence outside the framework runner.
type SessionManager struct {
	store              store.ConversationStore
	conversationID     int64
	model              string
	previousResponseID string
	lastTurnMeta       *store.TurnMeta
	lock               sync.Mutex
}

// NewSessionManager creates a new SessionManager for a conversation.
func NewSessionManager(s store.ConversationStore, convID int64, model string) *SessionManager {
	return &SessionManager{
		store:          s,
		conversationID: convID,
		model:          model,
	}
}

// LoadHistory loads persisted history for the conversation, strips replayed reasoning,
// sanitizes summary messages, derives previousResponseID, and marks messages persisted.
func (s *SessionManager) LoadHistory(ctx context.Context) ([]*schema.AgenticMessage, string, error) {
	summaryRow, tailRows, err := s.store.LoadHistory(ctx, s.conversationID)
	if err != nil {
		return nil, "", fmt.Errorf("load history: %w", err)
	}

	var historyMessages []*schema.AgenticMessage

	if summaryRow != nil {
		sMsgs, err := unmarshalTurn(summaryRow)
		if err != nil {
			return nil, "", err
		}
		for _, msg := range sMsgs {
			SanitizeSummaryMessage(msg)
		}
		historyMessages = append(historyMessages, sMsgs...)
	}

	for _, row := range tailRows {
		tMsgs, err := unmarshalTurn(row)
		if err != nil {
			return nil, "", err
		}
		historyMessages = append(historyMessages, tMsgs...)
	}

	var prevResponseID string
	if len(tailRows) > 0 {
		prevResponseID = tailRows[len(tailRows)-1].ResponseID
	} else if summaryRow != nil {
		prevResponseID = summaryRow.ResponseID
	}

	if clientPrevID, ok := GetPreviousResponseID(ctx); ok && clientPrevID != "" {
		prevResponseID = clientPrevID
	}

	s.lock.Lock()
	s.previousResponseID = prevResponseID
	s.lock.Unlock()

	for _, msg := range historyMessages {
		stripReplayReasoning(msg)
	}

	return historyMessages, prevResponseID, nil
}

// CommitTurn redacts secrets, extracts token usage & response ID,
// commits the turn to storage under a detached context, flags messages as persisted,
// and sets lastTurnMeta.
func (s *SessionManager) CommitTurn(ctx context.Context, messages []*schema.AgenticMessage) (*store.TurnMeta, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	question, answer := extractQuestionAndAnswer(messages)

	var prompt, completion, total int64
	var responseID string
	var finalAssistantMsg *schema.AgenticMessage

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == schema.AgenticRoleTypeAssistant {
			finalAssistantMsg = msg
			break
		}
	}

	if finalAssistantMsg != nil && finalAssistantMsg.ResponseMeta != nil && finalAssistantMsg.ResponseMeta.TokenUsage != nil {
		usage := finalAssistantMsg.ResponseMeta.TokenUsage
		prompt = int64(usage.PromptTokens)
		completion = int64(usage.CompletionTokens)
		total = int64(usage.TotalTokens)
		if total == 0 && (prompt > 0 || completion > 0) {
			total = prompt + completion
		}
	} else {
		var promptCharCount, completionCharCount int
		for _, msg := range messages {
			if msg == nil {
				continue
			}
			if msg == finalAssistantMsg {
				completionCharCount += tokens.MessageCharCount(msg)
			} else {
				promptCharCount += tokens.MessageCharCount(msg)
			}
		}
		prompt = int64(tokens.Estimate(promptCharCount))
		completion = int64(tokens.Estimate(completionCharCount))
		total = prompt + completion
	}

	if finalAssistantMsg != nil && finalAssistantMsg.Extra != nil {
		if einoID, ok := finalAssistantMsg.Extra["_eino_msg_id"].(string); ok && einoID != "" {
			if uuidRegex.MatchString(einoID) {
				responseID = einoID
			} else {
				log.Printf("SessionManager: invalid response ID %q (expected UUID), using empty fallback", einoID)
			}
		}
	}

	if responseID == "" {
		log.Printf("SessionManager: missing response ID for conversation %d, using empty fallback", s.conversationID)
	}

	var redactedMessages []*schema.AgenticMessage
	for _, msg := range messages {
		redactedMsg := tools.RedactAgenticMessage(msg)
		if redactedMsg.Extra == nil {
			redactedMsg.Extra = make(map[string]interface{})
		}
		redactedMsg.Extra[persistedKey] = true
		redactedMessages = append(redactedMessages, redactedMsg)
	}

	msgArrayJSONBytes, err := json.Marshal(redactedMessages)
	if err != nil {
		return nil, fmt.Errorf("marshal turn messages to JSON: %w", err)
	}
	msgArrayJSON := string(msgArrayJSONBytes)

	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), persistWriteTimeout)
	defer persistCancel()

	s.lock.Lock()
	prevID := s.previousResponseID
	s.lock.Unlock()

	seq, err := s.store.AppendTurn(
		persistCtx,
		s.conversationID,
		msgArrayJSON,
		responseID,
		prevID,
		s.model,
		prompt,
		completion,
		total,
		question,
		answer,
	)
	if err != nil {
		return nil, fmt.Errorf("append turn to store: %w", err)
	}

	for _, msg := range messages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		msg.Extra[persistedKey] = true
		msg.Extra["_onclaw_seq"] = seq
	}

	meta := &store.TurnMeta{
		ConversationID:     s.conversationID,
		SequenceNum:        seq,
		ResponseID:         responseID,
		PreviousResponseID: prevID,
		Model:              s.model,
		Tokens:             total,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
	}

	s.lock.Lock()
	s.lastTurnMeta = meta
	s.previousResponseID = responseID
	s.lock.Unlock()

	return meta, nil
}

// LastTurnMeta returns metadata from the most recently committed turn.
func (s *SessionManager) LastTurnMeta() *store.TurnMeta {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.lastTurnMeta
}

// SetPreviousResponseID overrides the stored previousResponseID.
func (s *SessionManager) SetPreviousResponseID(prevID string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.previousResponseID = prevID
}
