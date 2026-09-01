package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/modelcatalog"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// agentHandlers handles workspace agent CRUD, prompt regeneration, and memory endpoints.
type agentHandlers struct {
	store         store.Store
	encryptionKey []byte
	registry      *providers.Registry
	modelCatalog  *modelcatalog.Service
	agentService  *agents.Service
	workspaceDir  string
}

// NewAgentHandlers creates a new agentHandlers instance with injected dependencies.
func NewAgentHandlers(st store.Store, encryptionKey []byte, reg *providers.Registry, mc *modelcatalog.Service, as *agents.Service, workspaceDir string) *agentHandlers {
	return &agentHandlers{
		store:         st,
		encryptionKey: encryptionKey,
		registry:      reg,
		modelCatalog:  mc,
		agentService:  as,
		workspaceDir:  workspaceDir,
	}
}

// resolveAgent resolves an agent in the workspace by slug first, falling back to ID.
func (h *agentHandlers) resolveAgent(ctx context.Context, workspaceID, identifier string) (*domain.Agent, error) {
	if workspaceID == "" || identifier == "" {
		return nil, domain.ErrNotFound
	}
	agent, err := h.store.Agents().BySlug(ctx, workspaceID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	return h.store.Agents().ByID(ctx, workspaceID, identifier)
}

// composePromptDocuments fills an agent's identity/soul/bootstrap projection
// from the agent's workspace files under dir. Best-effort: a missing or
// unreadable file composes as empty — the status column carries the truth
// about readiness. The directory derives from the current root and slugs; the
// row records no path.
func composePromptDocuments(dir string, agent *domain.Agent) {
	agent.Identity, agent.Soul, agent.Bootstrap, _ = agents.ReadPromptDocuments(dir)
}

// ListAgents lists all agents in the current workspace.
func (h *agentHandlers) ListAgents(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	agentList, err := h.store.Agents().ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	// Compose-on-read is skipped for lists (design D4, summary roster);
	// prompt documents are fetched via the detail endpoint (GetAgent).

	RespondOK(c, gin.H{"agents": agentList})
}

// GetAgent retrieves a single agent by slug or ID.
func (h *agentHandlers) GetAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug), agent)
	RespondOK(c, gin.H{"agent": agent})
}

// CreateAgentRequest holds payload parameters for creating a new agent.
type CreateAgentRequest struct {
	Name        string                `json:"name"`
	Slug        string                `json:"slug"`
	Role        string                `json:"role"`
	Description string                `json:"description"`
	Brief       string                `json:"brief"`
	ProviderID  string                `json:"provider_id"`
	Model       string                `json:"model"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Effort      *string               `json:"effort,omitempty"`
	Autonomy    *domain.AgentAutonomy `json:"autonomy,omitempty"`
	Tools       []string              `json:"tools,omitempty"`
	Skills      []string              `json:"skills,omitempty"`
	MCP         []string              `json:"mcp,omitempty"`
	Avatar      json.RawMessage       `json:"avatar,omitempty"`
}

// CreateAgent creates a new agent in the current workspace and generates its prompts synchronously.
func (h *agentHandlers) CreateAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	agent, err := buildAgentFromCreateRequest(c.Request.Context(), ws.ID, user.ID, &req, h.store, h.registry, h.modelCatalog)
	if err != nil {
		RespondError(c, err)
		return
	}

	// The slug must be free before any filesystem or generation work: seeding
	// and generating into a taken directory would clobber a live agent's prompt
	// documents, and the post-insert cleanup would delete its directory.
	if _, err := h.store.Agents().BySlug(c.Request.Context(), ws.ID, agent.Slug); err == nil {
		RespondError(c, fmt.Errorf("%w: agent slug already exists in this workspace", domain.ErrConflict))
		return
	} else if !errors.Is(err, domain.ErrNotFound) {
		RespondError(c, err)
		return
	}

	// Assign the agent's on-disk workspace directory and seed it with the base
	// prompt before any DB write, so a failure rejects the request cleanly.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug)
	if err := agents.SeedWorkspace(agentDir); err != nil {
		RespondError(c, fmt.Errorf("failed to create agent workspace directory: %w", err))
		return
	}

	// Generate prompts BEFORE persisting: a failed generation aborts the create
	// with no agent row and no directory — the client keeps its form state and
	// the workspace never sees an error agent.
	// The generation service owns the generation budget; the context is
	// detached from the request so the request dying cannot strand state.
	if err := h.agentService.GenerateForCreate(context.Background(), agentDir, ws.ID, agent); err != nil {
		os.RemoveAll(agentDir)
		RespondError(c, fmt.Errorf("%w: %v", domain.ErrInvalid, agents.SanitizeError(err)))
		return
	}
	agent.PromptsStatus = domain.PromptsStatusReady

	if err := h.store.Agents().Create(c.Request.Context(), agent); err != nil {
		// No directory cleanup here: after the pre-check above, a conflict
		// means a raced create won the slug, and the directory now belongs to
		// that agent. An orphaned directory is inert and gets cleared by the
		// next SeedWorkspace for this slug.
		RespondError(c, err)
		return
	}

	// Refetch so the response carries store-assigned fields (id, timestamps).
	if refreshed, err := h.store.Agents().ByID(c.Request.Context(), ws.ID, agent.ID); err == nil {
		agent = refreshed
	}
	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, agent.Slug), agent)

	RespondCreated(c, gin.H{"agent": agent})
}

// PatchAgentRequest holds editable fields for updating an existing agent.
type PatchAgentRequest struct {
	Name        *string               `json:"name,omitempty"`
	Role        *string               `json:"role,omitempty"`
	Description *string               `json:"description,omitempty"`
	Brief       *string               `json:"brief,omitempty"`
	Identity    *string               `json:"identity,omitempty"`
	Soul        *string               `json:"soul,omitempty"`
	ProviderID  *string               `json:"provider_id,omitempty"`
	Model       *string               `json:"model,omitempty"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Effort      *string               `json:"effort,omitempty"`
	Autonomy    *domain.AgentAutonomy `json:"autonomy,omitempty"`
	Tools       *[]string             `json:"tools,omitempty"`
	Skills      *[]string             `json:"skills,omitempty"`
	MCP         *[]string             `json:"mcp,omitempty"`
	Avatar      *json.RawMessage      `json:"avatar,omitempty"`
}

// PatchAgent updates an existing agent's fields.
func (h *agentHandlers) PatchAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	param := c.Param("agent")

	var req PatchAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Role == nil && req.Description == nil &&
		req.Brief == nil && req.Identity == nil && req.Soul == nil && req.ProviderID == nil &&
		req.Model == nil && req.Temperature == nil && req.MaxTokens == nil && req.Effort == nil &&
		req.Autonomy == nil && req.Tools == nil && req.Skills == nil && req.MCP == nil && req.Avatar == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	existing, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Name = trimmed
	}

	if req.Name == nil && req.Role == nil && req.Description == nil &&
		req.Brief == nil && req.Identity == nil && req.Soul == nil && req.ProviderID == nil &&
		req.Model == nil && req.Temperature == nil && req.MaxTokens == nil && req.Effort == nil &&
		req.Autonomy == nil && req.Tools == nil && req.Skills == nil && req.MCP == nil && req.Avatar == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if req.Role != nil {
		trimmed := strings.TrimSpace(*req.Role)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: role cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Role = trimmed
	}

	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	if req.Brief != nil {
		trimmed := strings.TrimSpace(*req.Brief)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: brief cannot be empty", domain.ErrInvalid))
			return
		}
		existing.Brief = trimmed
	}

	targetProviderID := existing.ProviderID
	if req.ProviderID != nil {
		targetProviderID = strings.TrimSpace(*req.ProviderID)
		if targetProviderID == "" {
			RespondError(c, fmt.Errorf("%w: provider_id cannot be empty", domain.ErrInvalid))
			return
		}
	}

	targetModel := existing.Model
	if req.Model != nil {
		targetModel = strings.TrimSpace(*req.Model)
		if targetModel == "" {
			RespondError(c, fmt.Errorf("%w: model cannot be empty", domain.ErrInvalid))
			return
		}
	}

	provider, err := h.store.Providers().ByID(c.Request.Context(), ws.ID, targetProviderID)
	if err != nil {
		RespondError(c, fmt.Errorf("%w: provider not found in workspace", domain.ErrInvalid))
		return
	}

	var targetMaxTokens *int = existing.MaxTokens
	if req.MaxTokens != nil {
		targetMaxTokens = req.MaxTokens
	}

	providerImpl, err := h.registry.Get(provider.Type)
	if err != nil {
		RespondError(c, err)
		return
	}

	if providerImpl.RequiresMaxTokens() {
		if targetMaxTokens == nil || *targetMaxTokens <= 0 {
			RespondError(c, fmt.Errorf("%w: max_tokens is required for provider type %q", domain.ErrInvalid, provider.Type))
			return
		}
	}
	if targetMaxTokens != nil && *targetMaxTokens <= 0 {
		RespondError(c, fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid))
		return
	}

	var targetEffort *string = existing.Effort
	if req.Effort != nil {
		trimmed := strings.TrimSpace(*req.Effort)
		if trimmed == "" {
			targetEffort = nil
		} else {
			targetEffort = &trimmed
		}
	}
	if targetEffort != nil {
		if err := validateEffort(c.Request.Context(), h.modelCatalog, provider.Type, targetModel, *targetEffort); err != nil {
			RespondError(c, err)
			return
		}
	}

	existing.ProviderID = targetProviderID
	existing.Model = targetModel
	existing.MaxTokens = targetMaxTokens
	existing.Effort = targetEffort

	if req.Temperature != nil {
		if err := domain.ValidateAgentTemperature(*req.Temperature); err != nil {
			RespondError(c, err)
			return
		}
		existing.Temperature = *req.Temperature
	}

	if req.Autonomy != nil {
		if err := domain.ValidateAgentAutonomy(*req.Autonomy); err != nil {
			RespondError(c, err)
			return
		}
		existing.Autonomy = *req.Autonomy
	}

	if req.Avatar != nil {
		if err := domain.ValidateAgentAvatar(*req.Avatar); err != nil {
			RespondError(c, err)
			return
		}
		existing.Avatar = *req.Avatar
	}

	if req.Skills != nil {
		if err := validateAgentSkills(c.Request.Context(), h.store, ws.ID, *req.Skills); err != nil {
			RespondError(c, err)
			return
		}
		existing.Skills = *req.Skills
	}

	if req.Tools != nil {
		existing.Tools = *req.Tools
	}

	if req.MCP != nil {
		existing.MCP = *req.MCP
	}

	existing.UpdatedBy = &user.ID

	// Prompt documents are files: write whichever the payload carries before
	// the row update. Status stays untouched — manual edits before any
	// generation are allowed and a later regeneration overwrites the files.
	// The directory derives from the current root and slugs.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if req.Identity != nil {
		if err := agents.WritePromptDocument(agentDir, "IDENTITY.md", *req.Identity); err != nil {
			RespondError(c, fmt.Errorf("failed to write identity prompt document: %w", err))
			return
		}
	}
	if req.Soul != nil {
		if err := agents.WritePromptDocument(agentDir, "SOUL.md", *req.Soul); err != nil {
			RespondError(c, fmt.Errorf("failed to write soul prompt document: %w", err))
			return
		}
	}

	if err := h.store.Agents().Update(c.Request.Context(), existing); err != nil {
		RespondError(c, err)
		return
	}

	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug), existing)
	RespondOK(c, gin.H{"agent": existing})
}

// DeleteAgent deletes an agent and its per-user memories.
func (h *agentHandlers) DeleteAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	existing, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	if err := h.store.Agents().Delete(c.Request.Context(), ws.ID, existing.ID); err != nil {
		RespondError(c, err)
		return
	}

	// The agent's workspace directory dies with the row. The path derives from
	// the current root and slugs; no recorded path exists.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if err := os.RemoveAll(agentDir); err != nil {
		log.Printf("[handlers.agents] failed to remove agent workspace directory %s: %v", agentDir, err)
	}

	RespondNoContent(c)
}

// regenerateAgentRequest is the optional body for RegenerateAgent. An empty
// body or blank instruction regenerates without requested changes.
type regenerateAgentRequest struct {
	Instruction string `json:"instruction"`
}

// RegenerateAgent re-runs prompt generation synchronously from the stored
// brief, optionally steered by a client-supplied change instruction.
func (h *agentHandlers) RegenerateAgent(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")

	// The body is optional: an empty body regenerates without an instruction.
	var req regenerateAgentRequest
	_ = c.ShouldBindJSON(&req)

	existing, resolveErr := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if resolveErr != nil {
		RespondError(c, resolveErr)
		return
	}

	if existing.PromptsStatus == domain.PromptsStatusGenerating {
		RespondError(c, fmt.Errorf("%w: prompt generation is already in flight for this agent", domain.ErrConflict))
		return
	}

	if err := h.store.Agents().SetPromptState(c.Request.Context(), ws.ID, existing.ID, domain.PromptsStatusGenerating, nil); err != nil {
		RespondError(c, err)
		return
	}

	// Detached context: the generation service owns the budget and records the
	// final prompt state regardless of this request's lifetime.
	agentDir := domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug)
	if err := h.agentService.Generate(context.Background(), agentDir, ws.ID, existing.ID, req.Instruction); err != nil {
		log.Printf("[handlers.agents] prompt regeneration failed for agent %s/%s: %v", ws.ID, existing.ID, err)
	}

	// Refetch so the response reflects the final prompt state.
	if refreshed, err := h.store.Agents().ByID(c.Request.Context(), ws.ID, existing.ID); err == nil {
		existing = refreshed
	}
	composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, ws.Slug, existing.Slug), existing)

	RespondOK(c, gin.H{"agent": existing})
}

// GetAgentMemory returns the authenticated user's own memory for the specified agent.
func (h *agentHandlers) GetAgentMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	param := c.Param("agent")

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	mem, err := h.store.AgentUserMemories().Get(c.Request.Context(), ws.ID, agent.ID, user.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			RespondOK(c, gin.H{
				"agent_id":     agent.ID,
				"user_id":      user.ID,
				"workspace_id": ws.ID,
				"content":      "",
				"created_at":   nil,
				"updated_at":   nil,
			})
			return
		}
		RespondError(c, err)
		return
	}

	RespondOK(c, mem)
}

// DeleteAgentMemory resets (deletes) the authenticated user's own memory for the specified agent.
func (h *agentHandlers) DeleteAgentMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)
	param := c.Param("agent")

	agent, err := h.resolveAgent(c.Request.Context(), ws.ID, param)
	if err != nil {
		RespondError(c, err)
		return
	}

	err = h.store.AgentUserMemories().Delete(c.Request.Context(), ws.ID, agent.ID, user.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}

// buildAgentFromCreateRequest validates and constructs a domain.Agent from CreateAgentRequest.
func buildAgentFromCreateRequest(ctx context.Context, wsID, userID string, req *CreateAgentRequest, st store.Store, reg *providers.Registry, mc *modelcatalog.Service) (*domain.Agent, error) {
	if req == nil {
		return nil, domain.ErrInvalid
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: agent name is required", domain.ErrInvalid)
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		return nil, fmt.Errorf("%w: agent slug is required", domain.ErrInvalid)
	}
	if err := domain.ValidateAgentSlug(slug); err != nil {
		return nil, err
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		return nil, fmt.Errorf("%w: agent role is required", domain.ErrInvalid)
	}

	brief := strings.TrimSpace(req.Brief)
	if brief == "" {
		return nil, fmt.Errorf("%w: agent brief is required", domain.ErrInvalid)
	}

	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("%w: provider_id is required", domain.ErrInvalid)
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", domain.ErrInvalid)
	}

	// Validate provider belongs to workspace
	provider, err := st.Providers().ByID(ctx, wsID, providerID)
	if err != nil {
		return nil, fmt.Errorf("%w: provider not found in workspace", domain.ErrInvalid)
	}

	providerImpl, err := reg.Get(provider.Type)
	if err != nil {
		return nil, err
	}

	// Anthropic / capability max_tokens requirement
	if providerImpl.RequiresMaxTokens() {
		if req.MaxTokens == nil || *req.MaxTokens <= 0 {
			return nil, fmt.Errorf("%w: max_tokens is required for provider type %q", domain.ErrInvalid, provider.Type)
		}
	}
	if req.MaxTokens != nil && *req.MaxTokens <= 0 {
		return nil, fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid)
	}

	// Effort resolution validation
	if req.Effort != nil && strings.TrimSpace(*req.Effort) != "" {
		if err := validateEffort(ctx, mc, provider.Type, model, *req.Effort); err != nil {
			return nil, err
		}
	}

	temp := 1.0
	if req.Temperature != nil {
		if err := domain.ValidateAgentTemperature(*req.Temperature); err != nil {
			return nil, err
		}
		temp = *req.Temperature
	}

	autonomy := domain.AutonomyApproval
	if req.Autonomy != nil && *req.Autonomy != "" {
		if err := domain.ValidateAgentAutonomy(*req.Autonomy); err != nil {
			return nil, err
		}
		autonomy = *req.Autonomy
	}

	avatar := json.RawMessage("{}")
	if len(req.Avatar) > 0 {
		if err := domain.ValidateAgentAvatar(req.Avatar); err != nil {
			return nil, err
		}
		avatar = req.Avatar
	}

	if err := validateAgentSkills(ctx, st, wsID, req.Skills); err != nil {
		return nil, err
	}

	tools := req.Tools
	if tools == nil {
		tools = []string{}
	}
	skills := req.Skills
	if skills == nil {
		skills = []string{}
	}
	mcp := req.MCP
	if mcp == nil {
		mcp = []string{}
	}

	var effort *string
	if req.Effort != nil && strings.TrimSpace(*req.Effort) != "" {
		eff := strings.TrimSpace(*req.Effort)
		effort = &eff
	}

	var creator *string
	if userID != "" {
		creator = &userID
	}

	return &domain.Agent{
		WorkspaceID:   wsID,
		Slug:          slug,
		Name:          name,
		Role:          role,
		Description:   strings.TrimSpace(req.Description),
		Brief:         brief,
		ProviderID:    providerID,
		Model:         model,
		Temperature:   temp,
		MaxTokens:     req.MaxTokens,
		Effort:        effort,
		Autonomy:      autonomy,
		Tools:         tools,
		Skills:        skills,
		MCP:           mcp,
		Avatar:        avatar,
		PromptsStatus: domain.PromptsStatusGenerating,
		CreatedBy:     creator,
		UpdatedBy:     creator,
	}, nil
}

func validateEffort(ctx context.Context, mc *modelcatalog.Service, providerType, modelID, effort string) error {
	trimmed := strings.TrimSpace(effort)
	if trimmed == "" {
		return nil
	}

	validEfforts := mc.ResolveEfforts(ctx, providerType, modelID)

	if len(validEfforts) == 0 {
		return fmt.Errorf("%w: model %q does not support reasoning effort", domain.ErrInvalid, modelID)
	}

	for _, v := range validEfforts {
		if strings.EqualFold(v, trimmed) {
			return nil
		}
	}

	return fmt.Errorf("%w: invalid effort %q for model %q (valid: %s)", domain.ErrInvalid, trimmed, modelID, strings.Join(validEfforts, ", "))
}

func validateAgentSkills(ctx context.Context, st store.Store, workspaceID string, skills []string) error {
	if len(skills) == 0 {
		return nil
	}
	for _, s := range skills {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		_, err := st.WorkspaceSkills().FindByName(ctx, workspaceID, trimmed)
		if err != nil {
			return fmt.Errorf("%w: unknown skill %q in workspace", domain.ErrInvalid, trimmed)
		}
	}
	return nil
}
