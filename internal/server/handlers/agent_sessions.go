package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

// agentSessionView is one row of the per-user session listing
// (agent-session-index D3): exactly the index columns the sidebar renders
// plus the live-run flag. Workspace/agent/user attribution and the soft-delete
// marker never leave the server.
type agentSessionView struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	Running      bool      `json:"running"`
}

// agentSessionViews projects index rows onto the API view, computing each
// row's running flag as the intersection with the live-run registry (D3):
// one ActiveRunSessionIDs read for the whole listing, membership-tested per
// row. The projection returns an empty slice for empty rows so the response
// renders [] instead of null.
func agentSessionViews(rows []domain.AgentSession, liveSessionIDs []string) []agentSessionView {
	live := make(map[string]bool, len(liveSessionIDs))
	for _, id := range liveSessionIDs {
		live[id] = true
	}
	views := make([]agentSessionView, 0, len(rows))
	for _, row := range rows {
		views = append(views, agentSessionView{
			ID:           row.ID,
			SessionID:    row.SessionID,
			Title:        row.Title,
			CreatedAt:    row.CreatedAt,
			LastActiveAt: row.LastActiveAt,
			Running:      live[row.SessionID],
		})
	}
	return views
}

// ListAgentSessions serves the requesting user's durable session index for an
// agent (agent-session-index D3): only their own non-deleted rows for the
// workspace, newest activity first, each carrying the running flag from the
// run manager's live registry.
func (h *agentHandlers) ListAgentSessions(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}

	rows, err := h.agentSessions.ListAgentSessions(c.Request.Context(), ws.ID, agent.ID, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	var liveIDs []string
	if lister, ok := h.runner.(AgentRunSessionLister); ok {
		liveIDs = lister.ActiveRunSessionIDs(ws.ID, agent.ID)
	}

	RespondOK(c, gin.H{"sessions": agentSessionViews(rows, liveIDs)})
}

// DeleteAgentSession soft-deletes the caller's own index row
// (agent-session-index D6): the session disappears from listings while its
// transcript events and checkpoints stay on disk. A foreign-owned or absent
// row is domain.ErrNotFound — indistinguishable by design (no existence leak).
func (h *agentHandlers) DeleteAgentSession(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}

	if err := h.agentSessions.SoftDeleteAgentSession(c.Request.Context(), ws.ID, agent.ID, user.ID, c.Param("session")); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
