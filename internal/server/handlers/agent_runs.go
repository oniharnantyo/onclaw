package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// CancelRun cancels the live run for an agent session. Cancel addressing is
// session-scoped (one active run per session); the :turn path segment is
// accepted for API-shape stability but is not otherwise interpreted. The run
// unwinds at its next safe point and records a cancel marker in the session
// history; the response returns once cancellation has been initiated.
func (h *agentHandlers) CancelRun(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, c.Param("agent"))
	if err != nil {
		RespondError(c, err)
		return
	}

	sessionID := c.Param("session")
	turnID := c.Param("turn")
	if sessionID == "" || turnID == "" {
		RespondError(c, fmt.Errorf("%w: session and turn path segments are required", domain.ErrInvalid))
		return
	}

	// The session must exist in this workspace. Like the /v1 session binding,
	// a session with no persisted events is indistinguishable from an unknown
	// one: both are not-found.
	rows, err := h.sessionEvents.LoadEvents(c.Request.Context(), store.LoadSessionEventsParams{
		WorkspaceID: ws.ID,
		SessionID:   sessionID,
		Limit:       1,
	})
	if err != nil {
		RespondError(c, err)
		return
	}
	if len(rows) == 0 {
		RespondError(c, fmt.Errorf("%w: session not found", domain.ErrNotFound))
		return
	}

	if !h.runCanceler.CancelRun(ws.ID, agent.ID, sessionID) {
		RespondError(c, fmt.Errorf("%w: no live run for session %q", domain.ErrConflict, sessionID))
		return
	}

	RespondOK(c, gin.H{"cancelled": true})
}
