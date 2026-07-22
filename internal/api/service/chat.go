package service

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/conversation"
)

// Chat resolves the agent settings, ensures a conversation ID exists, and assembles the agent.
func (s *Service) Chat(ctx context.Context, input ChatInput) (int64, AssembledAgent, *conversation.SessionManager, error) {
	agentName := input.Agent
	if agentName == "" {
		defAgent, err := s.kv.Get(ctx, "default_agent")
		if err == nil && defAgent != "" {
			agentName = defAgent
		} else {
			agentName = "master"
		}
	}

	convID := input.ConversationID
	if convID == 0 {
		var err error
		convID, err = s.conv.CreateConversation(ctx, agentName)
		if err != nil {
			return 0, nil, nil, classify(err)
		}
	}

	assembledAgent, sessionMgr, _, err := s.resolve(ctx, agentName, input.Provider, input.Model, input.Reasoning, input.Workspace, convID)
	if err != nil {
		return 0, nil, nil, classify(err)
	}

	return convID, assembledAgent, sessionMgr, nil
}
