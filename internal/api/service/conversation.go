package service

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListConversations returns all conversations in the store.
func (s *Service) ListConversations(ctx context.Context) ([]*store.ConversationRow, error) {
	res, err := s.conv.ListConversations(ctx)
	return res, classify(err)
}

// ListMessagesResult holds a conversation's message history plus the resolved
// context window and compaction metadata for the web UI.
type ListMessagesResult struct {
	Messages         []*store.TurnRow
	ContextWindow    int64
	CompactionCount  int
	LastCompactionAt string
}

// ListMessages returns all message history, the resolved context window, and
// conversation-level compaction metadata for a given conversation ID.
func (s *Service) ListMessages(ctx context.Context, conversationID int64) (*ListMessagesResult, error) {
	res, err := s.conv.ListTurns(ctx, conversationID)
	if err != nil {
		return nil, classify(err)
	}

	// Resolve the conversation to find the agent name, then derive the
	// effective context window using the same precedence as the CLI run path
	// (agent MaxContextTokens > global default > model metadata > 64000) so the
	// web meter reflects the agent's configured limit.
	contextWindow := int64(store.ResolveContextWindow(0, s.globalMaxContextTokens, ""))
	convs, errConv := s.conv.ListConversations(ctx)
	if errConv == nil {
		var agentName string
		for _, c := range convs {
			if c.ID == conversationID {
				agentName = c.AgentName
				break
			}
		}
		if agentName != "" {
			agentConf, errAgent := s.mgr.GetAgent(ctx, agentName)
			if errAgent == nil && agentConf != nil {
				contextWindow = int64(store.ResolveContextWindow(agentConf.MaxContextTokens, s.globalMaxContextTokens, agentConf.ModelMetadata))
			}
		}
	}

	count, lastAt, err := s.conv.GetCompactionMeta(ctx, conversationID)
	if err != nil {
		return nil, classify(err)
	}

	return &ListMessagesResult{
		Messages:         res,
		ContextWindow:    contextWindow,
		CompactionCount:  count,
		LastCompactionAt: lastAt,
	}, nil
}
