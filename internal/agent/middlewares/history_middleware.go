package middlewares

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agent/tools"
	"github.com/oniharnantyo/onclaw/internal/store"
)

type contextKey string

const (
	cursorKey    = contextKey("onclaw_history_cursor")
	persistedKey = "_onclaw_persisted"
)

// RunCursor tracks the max sequence number within a single run.
type RunCursor struct {
	MaxSeq int64
}

// HistoryMiddleware embeds Eino's base middleware and persists/replays conversation history.
type HistoryMiddleware struct {
	adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	Store          store.ConversationStore
	ConversationID int64
}

// NewHistoryMiddleware creates a new HistoryMiddleware.
func NewHistoryMiddleware(s store.ConversationStore, convID int64) *HistoryMiddleware {
	return &HistoryMiddleware{
		Store:          s,
		ConversationID: convID,
	}
}

func (h *HistoryMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext[*schema.AgenticMessage]) (context.Context, *adk.ChatModelAgentContext[*schema.AgenticMessage], error) {
	// 1. Load History
	summaryRow, tailRows, err := h.Store.LoadHistory(ctx, h.ConversationID)
	if err != nil {
		return ctx, nil, fmt.Errorf("load history: %w", err)
	}

	var historyMessages []*schema.AgenticMessage

	unmarshalMsg := func(row *store.MessageRow) (*schema.AgenticMessage, error) {
		var msg schema.AgenticMessage
		if err := json.Unmarshal([]byte(row.Message), &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message row: %w", err)
		}
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		msg.Extra[persistedKey] = true
		msg.Extra["_onclaw_seq"] = row.Seq
		return &msg, nil
	}

	if summaryRow != nil {
		sMsg, err := unmarshalMsg(summaryRow)
		if err != nil {
			return ctx, nil, err
		}
		historyMessages = append(historyMessages, sMsg)
	}

	for _, row := range tailRows {
		tMsg, err := unmarshalMsg(row)
		if err != nil {
			return ctx, nil, err
		}
		historyMessages = append(historyMessages, tMsg)
	}

	// Inject history before the user input messages
	originalMessages := runCtx.AgentInput.Messages
	runCtx.AgentInput.Messages = append(historyMessages, originalMessages...)

	// Determine max sequence number from loaded history
	var maxSeq int64
	if len(tailRows) > 0 {
		maxSeq = tailRows[len(tailRows)-1].Seq
	} else if summaryRow != nil {
		maxSeq = summaryRow.Seq
	}

	// Save and mark the new user messages immediately
	for _, msg := range originalMessages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		if !IsPersisted(msg) {
			seq, err := h.saveMessage(ctx, msg)
			if err != nil {
				return ctx, nil, fmt.Errorf("save user message: %w", err)
			}
			msg.Extra[persistedKey] = true
			msg.Extra["_onclaw_seq"] = seq
			if seq > maxSeq {
				maxSeq = seq
			}
		}
	}

	// Initialize the run cursor in context
	cursor := &RunCursor{MaxSeq: maxSeq}
	ctx = context.WithValue(ctx, cursorKey, cursor)

	return ctx, runCtx, nil
}

func (h *HistoryMiddleware) AfterModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], modelCtx *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	if err := h.saveUnmarkedMessages(ctx, state.Messages); err != nil {
		return ctx, nil, fmt.Errorf("after model rewrite state: %w", err)
	}
	return ctx, state, nil
}

func (h *HistoryMiddleware) AfterAgent(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage]) (context.Context, error) {
	if err := h.saveUnmarkedMessages(ctx, state.Messages); err != nil {
		return ctx, fmt.Errorf("after agent: %w", err)
	}
	return ctx, nil
}

func (h *HistoryMiddleware) saveUnmarkedMessages(ctx context.Context, stateMessages []*schema.AgenticMessage) error {
	cursor, ok := ctx.Value(cursorKey).(*RunCursor)
	if !ok {
		return nil
	}

	for _, msg := range stateMessages {
		if msg.Extra == nil {
			msg.Extra = make(map[string]interface{})
		}
		if !IsPersisted(msg) {
			seq, err := h.saveMessage(ctx, msg)
			if err != nil {
				return err
			}
			msg.Extra[persistedKey] = true
			msg.Extra["_onclaw_seq"] = seq
			if seq > cursor.MaxSeq {
				cursor.MaxSeq = seq
			}
		}
	}
	return nil
}

func (h *HistoryMiddleware) saveMessage(ctx context.Context, msg *schema.AgenticMessage) (int64, error) {
	redactedMsg := tools.RedactAgenticMessage(msg)
	if redactedMsg.Extra == nil {
		redactedMsg.Extra = make(map[string]interface{})
	}
	redactedMsg.Extra[persistedKey] = true

	messageJSON, err := json.Marshal(redactedMsg)
	if err != nil {
		return 0, fmt.Errorf("marshal message to JSON: %w", err)
	}

	seq, err := h.Store.AppendMessage(ctx, h.ConversationID, string(msg.Role), string(messageJSON))
	if err != nil {
		return 0, fmt.Errorf("append message to store: %w", err)
	}

	return seq, nil
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
