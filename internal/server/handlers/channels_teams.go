package handlers

// Channel-teams surface (OpenSpec change channel-teams, design D2/D6, tasks
// 6–7): team-template listing and materialization, work-session reads, and
// the agent spawner that binds spawn-slots through the shared creation path.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/teams"
)

// spawnedAgentDefaultModel is the model bound to template-spawned agents when
// the materialize flow resolves the workspace's first provider. The mock and
// catalog-backed providers in tests and smoke both serve it; a spawned agent
// can be re-pointed through the normal agent PATCH afterwards.
const spawnedAgentDefaultModel = "gpt-4o"

// templateSlotView is the read view of one template slot on the listing
// endpoint (channel-teams task 6.3): the materialization UI binds slots by id.
type templateSlotView struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Specialization string `json:"specialization"`
	Facilitator    bool   `json:"facilitator"`
}

// templateView is the read view of one built-in template. The conventions
// prefill and role prompts ride along so the flow can preview what
// materialization will create.
type templateView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Conventions string             `json:"conventions"`
	Slots       []templateSlotView `json:"slots"`
}

// ListChannelTemplates returns the built-in team templates (design D6: no
// template table in v1 — code-defined built-ins only).
func (h *channelHandlers) ListChannelTemplates(c *gin.Context) {
	builtins := teams.BuiltIn()
	items := make([]templateView, 0, len(builtins))
	for _, t := range builtins {
		items = append(items, templateViewOf(t))
	}
	RespondOK(c, gin.H{"templates": items})
}

// templateViewOf projects a template onto its read view.
func templateViewOf(t teams.TeamTemplate) templateView {
	slots := make([]templateSlotView, 0, len(t.Slots))
	for _, s := range t.Slots {
		slots = append(slots, templateSlotView{
			ID:             s.ID,
			Title:          s.Title,
			Specialization: s.Specialization,
			Facilitator:    s.Facilitator,
		})
	}
	return templateView{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Conventions: t.Conventions,
		Slots:       slots,
	}
}

// templateMaterializeRequest is the materialize payload: channel identity
// plus one binding per template slot, each exactly one of agent_id (an
// existing workspace agent) or spawn.
type templateMaterializeRequest struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Purpose string `json:"purpose"`
	Slots   map[string]struct {
		AgentID string `json:"agent_id"`
		Spawn   bool   `json:"spawn"`
	} `json:"slots"`
}

// MaterializeTemplate materializes a built-in template into a channel
// (channel-teams D6): 201 with {channel, members, created_agents}; unknown
// templates are 404; payload problems are fielded 422; a taken channel slug
// is 409.
func (h *channelHandlers) MaterializeTemplate(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	templateID := c.Param("template")

	if _, ok := teams.Get(templateID); !ok {
		AbortNotFound(c, fmt.Sprintf("team template %q does not exist", templateID))
		return
	}

	var req templateMaterializeRequest
	if !bindChannelRequest(c, &req) {
		return
	}

	slots := make(map[string]teams.SlotBinding, len(req.Slots))
	for id, binding := range req.Slots {
		slots[id] = teams.SlotBinding{AgentID: binding.AgentID, Spawn: binding.Spawn}
	}

	result, err := h.materializer.Materialize(c.Request.Context(), ws.ID, user.ID, teams.MaterializeRequest{
		TemplateID: templateID,
		Name:       req.Name,
		Slug:       req.Slug,
		Purpose:    req.Purpose,
		Slots:      slots,
	})
	if err != nil {
		respondMaterializeError(c, err)
		return
	}

	// The roster comes back through the resolved member view (display names +
	// @handles) so the response matches the roster endpoint's shape.
	rows, rerr := h.channels.ListChannelMembers(c.Request.Context(), ws.ID, result.Channel.ID)
	if rerr != nil {
		RespondError(c, rerr)
		return
	}
	views := make([]channelMemberView, 0, len(rows))
	for i := range rows {
		views = append(views, h.memberView(c.Request.Context(), &rows[i]))
	}

	if result.CreatedAgents == nil {
		result.CreatedAgents = []domain.Agent{}
	}
	RespondJSON(c, http.StatusCreated, gin.H{
		"channel":        result.Channel,
		"members":        views,
		"created_agents": result.CreatedAgents,
	})
}

// respondMaterializeError maps materializer failures: fielded validation
// errors are 422s with their details; everything else (unknown template,
// foreign agents, slug conflicts) rides the sentinel chain.
func respondMaterializeError(c *gin.Context, err error) {
	var verr *teams.ValidationError
	if errors.As(err, &verr) {
		details := make([]ErrorDetail, 0, len(verr.Fields))
		for _, f := range verr.Fields {
			details = append(details, ErrorDetail{Field: f.Field, Message: f.Message})
		}
		AbortWithError(c, http.StatusUnprocessableEntity, CodeInvalidRequest, verr.Error(), details...)
		return
	}
	RespondError(c, err)
}

// -------------------------------------------------------------------------
// Work sessions (channel-teams task 7.1)
// -------------------------------------------------------------------------

// ListChannelSessions returns the channel's work sessions, newest first.
func (h *channelHandlers) ListChannelSessions(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	rows, err := h.sessions.ListWorkSessions(c.Request.Context(), ws.ID, channel.ID)
	if err != nil {
		RespondError(c, err)
		return
	}
	if rows == nil {
		rows = []domain.WorkSession{}
	}
	RespondOK(c, gin.H{"sessions": rows})
}

// GetChannelSession returns one work session's detail: status, pause reason,
// budget, hops used, summary, and close time.
func (h *channelHandlers) GetChannelSession(c *gin.Context) {
	ws, channel, ok := h.resolveChannel(c)
	if !ok {
		return
	}
	session, err := h.sessions.GetWorkSession(c.Request.Context(), ws.ID, c.Param("sid"))
	if err != nil {
		RespondError(c, err)
		return
	}
	// A session from another channel in the same workspace is still not this
	// channel's session — 404, matching the channel isolation convention.
	if session.ChannelID != channel.ID {
		RespondError(c, domain.ErrNotFound)
		return
	}
	RespondOK(c, gin.H{"session": session})
}

// -------------------------------------------------------------------------
// Agent spawner (the teams.AgentCreator implementation)
// -------------------------------------------------------------------------

// TeamsAgentSpawner binds spawn-slots through the shared agent creation path
// (AgentCreationDeps): template-spawned agents get the same slug pre-check,
// workspace seeding, role-informed prompt generation, and persistence as
// REST-created agents. When the spawn request carries no provider, the
// workspace default model pair is resolved when set, falling back to the
// first workspace provider with spawnedAgentDefaultModel.
type TeamsAgentSpawner struct {
	deps       AgentCreationDeps
	providers  store.ProviderStore
	workspaces store.WorkspaceStore
}

// NewTeamsAgentSpawner creates the spawner from its positional dependencies.
func NewTeamsAgentSpawner(deps AgentCreationDeps, providers store.ProviderStore, workspaces store.WorkspaceStore) *TeamsAgentSpawner {
	return &TeamsAgentSpawner{deps: deps, providers: providers, workspaces: workspaces}
}

// SpawnAgent implements teams.AgentCreator.
func (s *TeamsAgentSpawner) SpawnAgent(ctx context.Context, req teams.SpawnAgentRequest) (domain.Agent, error) {
	ws, err := s.workspaces.ByID(ctx, req.WorkspaceID)
	if err != nil {
		return domain.Agent{}, err
	}

	create := CreateAgentRequest{
		Name:        req.Name,
		Slug:        req.Slug,
		Role:        req.Role,
		Description: req.Description,
		Brief:       req.Brief,
		ProviderID:  req.ProviderID,
		Model:       req.Model,
	}
	if create.ProviderID == "" {
		providerID, model, err := s.defaultProvider(ctx, ws, create.Model)
		if err != nil {
			return domain.Agent{}, err
		}
		create.ProviderID = providerID
		create.Model = model
	}

	agent, err := s.deps.CreateAgentRecord(ctx, req.WorkspaceID, ws.Slug, req.UserID, &create, ws.DefaultModel)
	if err != nil {
		return domain.Agent{}, err
	}
	return *agent, nil
}

// defaultProvider resolves the spawned agent's pinned pair when the template
// names no provider: the workspace default model when set, else the first
// workspace provider with spawnedAgentDefaultModel. An explicitly templated
// model wins over both defaults.
func (s *TeamsAgentSpawner) defaultProvider(ctx context.Context, ws *domain.Workspace, model string) (string, string, error) {
	if ws.DefaultModel != nil && ws.DefaultModel.ProviderID != "" && ws.DefaultModel.Model != "" {
		if strings.TrimSpace(model) == "" {
			model = ws.DefaultModel.Model
		}
		return ws.DefaultModel.ProviderID, model, nil
	}

	rows, err := s.providers.ListForWorkspace(ctx, ws.ID)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", fmt.Errorf("%w: the workspace has no provider configured; add one before spawning template agents", domain.ErrInvalid)
	}
	if strings.TrimSpace(model) == "" {
		model = spawnedAgentDefaultModel
	}
	return rows[0].ID, model, nil
}
