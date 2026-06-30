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

// ListMessages returns all message history for a given conversation ID.
func (s *Service) ListMessages(ctx context.Context, conversationID int64) ([]*store.MessageRow, error) {
	res, err := s.conv.ListMessages(ctx, conversationID)
	return res, classify(err)
}
