package handlers

// Agent heartbeat REST surface (add-agent-heartbeat D13): the four agent
// sub-resource endpoints — GET/PUT /agents/:agent/heartbeat and POST
// /agents/:agent/heartbeat/{run-now,resume} — riding the agents permissions
// (agents.read for the read, agents.write for every mutation; no new
// permission catalog entries, D13). The heartbeat is exactly one row per
// agent, so there is no listing or delete surface. next_tick_at is derived
// state: recomputed here in the workspace timezone on every enabled save and
// cleared on pause — never accepted from request input (D4). The run-now and
// resume dispatches go through the heartbeat service (the same instance the
// ticker uses), whose sentinel errors ride the generic mapping (its
// ErrNotFound wraps domain.ErrNotFound; an in-flight tick wraps
// domain.ErrConflict → 409).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// HeartbeatDispatch is the run-now/resume face the mutation endpoints use
// (add-agent-heartbeat D13). *heartbeat.Service satisfies it; kept narrow so
// the handlers test the mapping without the ticker.
type HeartbeatDispatch interface {
	RunNow(ctx context.Context, workspaceID, agentID string) (*domain.HeartbeatRun, error)
	Resume(ctx context.Context, workspaceID, agentID string) (*domain.Heartbeat, error)
}

// heartbeatPutRequest is the PUT payload (the web HeartbeatPayload). Prompt
// is verbatim: an explicit empty checklist stays empty (skip-until-edited,
// D3) — only a first-ever save seeds the template. next_tick_at is not a
// payload field (D4).
type heartbeatPutRequest struct {
	Enabled     bool                      `json:"enabled"`
	Expr        string                    `json:"expr"`
	ActiveStart *string                   `json:"active_start"`
	ActiveEnd   *string                   `json:"active_end"`
	Delivery    *domain.HeartbeatDelivery `json:"delivery"`
	Prompt      string                    `json:"prompt"`
}

// heartbeatView is the read view the web lib expects: the raw row plus the
// server-derived human_label (the scheduler read-view convention, D2 — never
// stored from input).
type heartbeatView struct {
	domain.Heartbeat
	HumanLabel string `json:"human_label"`
}

func heartbeatViewOf(hb *domain.Heartbeat) heartbeatView {
	return heartbeatView{Heartbeat: *hb, HumanLabel: domain.HumanLabel(hb.Expr)}
}

// heartbeatFieldFor maps a domain.ValidateHeartbeat error onto the payload
// field it names — a presentation hint over the domain's stable error strings
// (the schedulerFieldFor pattern; one validator, two surfaces).
func heartbeatFieldFor(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "active_start"):
		return "active_start"
	case strings.Contains(msg, "active_end") || strings.Contains(msg, "active hours must not be equal"):
		return "active_end"
	case strings.Contains(msg, "active hours"):
		return "active_start"
	case strings.Contains(msg, "cron expression") || strings.Contains(msg, "fires faster"):
		return "expr"
	case strings.Contains(msg, "delivery type"):
		return "delivery.type"
	case strings.Contains(msg, "channel_id"):
		return "delivery.channel_id"
	default:
		return ""
	}
}

// respondHeartbeatValidation writes the fielded 422 envelope (the scheduler
// convention) for heartbeat payloads.
func respondHeartbeatValidation(c *gin.Context, err error) {
	detail := ErrorDetail{Field: heartbeatFieldFor(err), Message: err.Error()}
	AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, detail.Message, detail)
}

// agentHeartbeatHandlers serves one agent's heartbeat surface
// (add-agent-heartbeat D13). Dependencies are granular: the agents store for
// the existence 404, the heartbeats store for the row, the workspaces store
// for the timezone the next tick is computed in, and the dispatch face (the
// heartbeat service) for run-now/resume.
type agentHeartbeatHandlers struct {
	agents     store.AgentStore
	heartbeats store.HeartbeatStore
	workspaces store.WorkspaceStore
	dispatch   HeartbeatDispatch
}

// NewAgentHeartbeatHandlers creates a new agentHeartbeatHandlers instance.
func NewAgentHeartbeatHandlers(agents store.AgentStore, heartbeats store.HeartbeatStore, workspaces store.WorkspaceStore, dispatch HeartbeatDispatch) *agentHeartbeatHandlers {
	return &agentHeartbeatHandlers{agents: agents, heartbeats: heartbeats, workspaces: workspaces, dispatch: dispatch}
}

// resolveHeartbeatAgent resolves the :agent param by slug then id under the
// workspace (the agent endpoints' convention); a foreign workspace's agent is
// indistinguishable from unknown (404).
func (h *agentHeartbeatHandlers) resolveHeartbeatAgent(c *gin.Context, workspaceID, identifier string) (*domain.Agent, bool) {
	if workspaceID == "" || identifier == "" {
		RespondError(c, domain.ErrNotFound)
		return nil, false
	}
	agent, err := h.agents.BySlug(c.Request.Context(), workspaceID, identifier)
	if err == nil {
		return agent, true
	}
	if !errors.Is(err, domain.ErrNotFound) {
		RespondError(c, err)
		return nil, false
	}
	agent, err = h.agents.ByID(c.Request.Context(), workspaceID, identifier)
	if err != nil {
		RespondError(c, err)
		return nil, false
	}
	return agent, true
}

// GetAgentHeartbeat returns the agent's heartbeat — null when the agent has
// none (the never-created state is normal, not an error) — plus the embedded
// default checklist template for the UI's reset-to-default affordance (D3).
func (h *agentHeartbeatHandlers) GetAgentHeartbeat(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveHeartbeatAgent(c, ws.ID, c.Param("agent"))
	if !ok {
		return
	}
	hb, err := h.heartbeats.GetHeartbeat(c.Request.Context(), ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	var view *heartbeatView
	if hb != nil {
		v := heartbeatViewOf(hb)
		view = &v
	}
	RespondOK(c, gin.H{"heartbeat": view, "default_prompt": promptdocs.HeartbeatTemplate})
}

// PutAgentHeartbeat create-or-updates the agent's single heartbeat: load the
// existing row (nil = creating), overlay the payload, validate, recompute the
// derived next tick, persist through the create-or-replace port. A first save
// with an empty prompt seeds the embedded template (D3); an update keeps the
// given prompt verbatim — an explicit clear stays cleared. Pausing clears the
// next tick; enabling lands on the next future occurrence in the workspace
// timezone (D4).
func (h *agentHeartbeatHandlers) PutAgentHeartbeat(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	agent, ok := h.resolveHeartbeatAgent(c, ws.ID, c.Param("agent"))
	if !ok {
		return
	}

	var req heartbeatPutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	existing, err := h.heartbeats.GetHeartbeat(c.Request.Context(), ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	prompt := req.Prompt
	if existing == nil && strings.TrimSpace(prompt) == "" {
		// First save with no checklist: seed the embedded template (D3) so a
		// fresh heartbeat is editable-then-live rather than silently skipping.
		prompt = promptdocs.HeartbeatTemplate
	}

	createdBy := &user.ID
	failureStreak := 0
	var lastTick *domain.HeartbeatLastRun
	if existing != nil {
		// Identity and earned state ride the existing row: created_by is
		// immutable, and the failure streak / last-tick snapshot are runtime
		// accounting a payload edit must not reset (D12's five-strike).
		createdBy = existing.CreatedBy
		failureStreak = existing.FailureStreak
		lastTick = existing.LastTick
	}
	if createdBy == nil {
		createdBy = &user.ID
	}

	now := time.Now()
	hb := &domain.Heartbeat{
		WorkspaceID:   ws.ID,
		AgentID:       agent.ID,
		CreatedBy:     createdBy,
		Prompt:        prompt,
		Expr:          req.Expr,
		ActiveStart:   req.ActiveStart,
		ActiveEnd:     req.ActiveEnd,
		Enabled:       req.Enabled,
		FailureStreak: failureStreak,
		LastTick:      lastTick,
	}
	if req.Delivery != nil {
		hb.Delivery = *req.Delivery
	}

	if err := domain.ValidateHeartbeat(hb, now); err != nil {
		respondHeartbeatValidation(c, err)
		return
	}

	// Derived runtime state (D4): computed here in the workspace timezone —
	// pausing clears it, enabling lands on the next future occurrence.
	if hb.Enabled {
		next, err := domain.NextRun(hb.Expr, now, workspaceTimezone(ws))
		if err != nil {
			respondHeartbeatValidation(c, err)
			return
		}
		hb.NextTickAt = next
	}

	if err := h.heartbeats.PutHeartbeat(c.Request.Context(), ws.ID, agent.ID, hb); err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"heartbeat": heartbeatViewOf(hb)})
}

// RunAgentHeartbeatNow fires one tick immediately (D13): through the same
// service instance the ticker uses, so guards, the shared hb_ session, and
// delivery behave identically. An in-flight tick is 409; an agent without a
// heartbeat (or an unknown agent) is 404.
func (h *agentHeartbeatHandlers) RunAgentHeartbeatNow(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveHeartbeatAgent(c, ws.ID, c.Param("agent"))
	if !ok {
		return
	}
	run, err := h.dispatch.RunNow(c.Request.Context(), ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"run": run})
}

// ResumeAgentHeartbeat re-enables a paused heartbeat (D12): through the
// service, which recomputes next_tick_at from now in the workspace timezone
// and resets the failure streak. Absent is 404.
func (h *agentHeartbeatHandlers) ResumeAgentHeartbeat(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	agent, ok := h.resolveHeartbeatAgent(c, ws.ID, c.Param("agent"))
	if !ok {
		return
	}
	hb, err := h.dispatch.Resume(c.Request.Context(), ws.ID, agent.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	RespondOK(c, gin.H{"heartbeat": heartbeatViewOf(hb)})
}
