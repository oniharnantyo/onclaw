package server

// Production @handle directory for the channel chokepoint's mention
// resolution (integrate-agent-channels D3; channel-teams D3/D4). The rules
// are the shared ones — agents by slug, humans by the dashed lowercase
// display name — kept in lockstep with the runner's per-run resolver
// (internal/agents channels.go humanChannelHandle) and the roster views'
// userHandle helper (internal/server/handlers channels.go).

import (
	"context"
	"fmt"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// storeChannelHandles resolves roster members' @handles from the user and
// agent stores. It is long-lived (the chokepoint resolves on every post), so
// it reads through on each call — roster edits surface immediately.
type storeChannelHandles struct {
	users  store.UserStore
	agents store.AgentStore
}

// newStoreChannelHandles creates the handles directory from its positional
// store dependencies.
func newStoreChannelHandles(users store.UserStore, agents store.AgentStore) *storeChannelHandles {
	return &storeChannelHandles{users: users, agents: agents}
}

// UserHandle implements agents.ChannelHandles: the dashed lowercase display
// name.
func (h *storeChannelHandles) UserHandle(ctx context.Context, userID string) (string, error) {
	user, err := h.users.ByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("resolve user handle: %w", err)
	}
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(user.Name), " ", "-")), nil
}

// AgentHandle implements agents.ChannelHandles: the agent slug.
func (h *storeChannelHandles) AgentHandle(ctx context.Context, workspaceID, agentID string) (string, error) {
	agent, err := h.agents.ByID(ctx, workspaceID, agentID)
	if err != nil {
		return "", fmt.Errorf("resolve agent handle: %w", err)
	}
	return agent.Slug, nil
}
