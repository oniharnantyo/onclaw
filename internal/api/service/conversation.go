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

// ListMessages returns all message history and resolved context window for a given conversation ID.
func (s *Service) ListMessages(ctx context.Context, conversationID int64) ([]*store.TurnRow, int64, error) {
	res, err := s.conv.ListTurns(ctx, conversationID)
	if err != nil {
		return nil, 0, classify(err)
	}

	// Default context window to 64000
	contextWindow := int64(64000)

	// Resolve the conversation to find the agent name
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
				if agentConf.ModelMetadata != "" {
					meta, errMeta := store.UnmarshalModelMetadata(agentConf.ModelMetadata)
					if errMeta == nil && meta != nil && meta.ContextWindow > 0 {
						contextWindow = int64(meta.ContextWindow)
					}
				}
			}
		}
	}

	return res, contextWindow, nil
}
