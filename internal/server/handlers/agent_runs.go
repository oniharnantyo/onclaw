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
//
// The session-ownership rule (fix-role-permission-audit design D3) permits
// the session's owning member or an agents.write holder; everyone else is
// 403 — checked before the live-run probe so a member cannot interrupt
// another member's run.
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

	allowed, _, err := h.sessionManageAllowed(c, ws, agent.ID, sessionID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if !allowed {
		AbortForbidden(c, "cancelling another member's run requires the "+domain.AgentsWrite+" permission")
		return
	}

	// A live run proves the session exists — cancel it even when its events
	// have not been committed yet. A stop during the session's first turn
	// (nothing persisted before the run's terminal event) would otherwise 404
	// and silently leave the run streaming to completion.
	if h.runCanceler.CancelRun(ws.ID, agent.ID, sessionID) {
		RespondOK(c, gin.H{"cancelled": true})
		return
	}

	// No live run: the session-existence check decides the error. Like the
	// /v1 session binding, a session with no persisted events is
	// indistinguishable from an unknown one — both are not-found; a known
	// session with no live run is a conflict.
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

	RespondError(c, fmt.Errorf("%w: no live run for session %q", domain.ErrConflict, sessionID))
}
