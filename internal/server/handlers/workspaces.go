package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/promptdocs"
	"github.com/oniharnantyo/onclaw/internal/promptgen"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/services"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// workspaceHandlers handles workspace lifecycle and retrieval endpoints.
type workspaceHandlers struct {
	store         store.Store
	encryptionKey []byte
	registry      *providers.Registry
	modelCatalog  *services.ModelCatalog
	agentService  *promptgen.Service
	workspaceDir  string
}

// NewWorkspaceHandlers creates a new workspaceHandlers instance with injected dependencies.
func NewWorkspaceHandlers(st store.Store, encryptionKey []byte, reg *providers.Registry, mc *services.ModelCatalog, as *promptgen.Service, workspaceDir string) *workspaceHandlers {
	if reg == nil {
		reg = providers.NewRegistry()
	}
	return &workspaceHandlers{
		store:         st,
		encryptionKey: encryptionKey,
		registry:      reg,
		modelCatalog:  mc,
		agentService:  as,
		workspaceDir:  workspaceDir,
	}
}

// ListWorkspaces lists all workspaces and roles for the authenticated user.
func (h *workspaceHandlers) ListWorkspaces(c *gin.Context) {
	user := MustCurrentUser(c)

	memberships, err := h.store.Members().ListForUser(c.Request.Context(), user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspaces": memberships})
}

// CreateWorkspaceRequest holds the payload for creating a new workspace,
// optionally including a provider configuration and starter agent for atomic birth.
type CreateWorkspaceRequest struct {
	Name         string                 `json:"name"`
	Slug         string                 `json:"slug"`
	Description  *string                `json:"description,omitempty"`
	Timezone     string                 `json:"timezone"`
	Provider     *CreateProviderRequest `json:"provider,omitempty"`
	StarterAgent *CreateAgentRequest    `json:"starter_agent,omitempty"`
}

// CreateWorkspace creates a new workspace, seeds the three built-in roles,
// assigns the authenticated creator as the Owner, and optionally creates the provider
// and starter agent within a single transaction (atomic birth).
func (h *workspaceHandlers) CreateWorkspace(c *gin.Context) {
	user := MustCurrentUser(c)

	var req CreateWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		RespondError(c, fmt.Errorf("%w: workspace name cannot be empty", domain.ErrInvalid))
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		RespondError(c, fmt.Errorf("%w: workspace slug cannot be empty", domain.ErrInvalid))
		return
	}

	if err := domain.ValidateSlug(slug); err != nil {
		RespondError(c, err)
		return
	}

	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "UTC"
	}

	if req.StarterAgent != nil && req.Provider == nil {
		RespondError(c, fmt.Errorf("%w: provider is required when starter_agent is provided", domain.ErrInvalid))
		return
	}

	var pType, pName, baseURL, catalogProvider string
	var pEnabled bool = true
	var providerImpl providers.Provider

	if req.Provider != nil {
		pType = strings.TrimSpace(req.Provider.Type)
		var err error
		providerImpl, err = h.registry.Get(pType)
		if err != nil {
			RespondError(c, err)
			return
		}

		pName = strings.TrimSpace(req.Provider.Name)
		if pName == "" {
			RespondError(c, fmt.Errorf("%w: provider name is required", domain.ErrInvalid))
			return
		}

		if req.Provider.BaseURL != nil {
			baseURL = strings.TrimSpace(*req.Provider.BaseURL)
		}

		if providerImpl.RequiresBaseURL() && baseURL == "" {
			RespondError(c, fmt.Errorf("%w: base_url is required for provider type %q", domain.ErrInvalid, pType))
			return
		}

		if baseURL != "" {
			if err := validateBaseURL(baseURL); err != nil {
				RespondError(c, err)
				return
			}
		}

		if req.Provider.CatalogProvider != nil {
			catalogProvider = strings.TrimSpace(*req.Provider.CatalogProvider)
			if err := validateCatalogHint(c.Request.Context(), h.modelCatalog, catalogProvider); err != nil {
				RespondError(c, err)
				return
			}
		}

		if req.Provider.Enabled != nil {
			pEnabled = *req.Provider.Enabled
		}
	}

	var agentSlug, agentName, agentRole, agentBrief, agentModel string
	var temp float64 = 1.0
	var autonomy domain.AgentAutonomy = domain.AutonomyApproval
	var avatar json.RawMessage = json.RawMessage("{}")

	if req.StarterAgent != nil {
		agentName = strings.TrimSpace(req.StarterAgent.Name)
		if agentName == "" {
			RespondError(c, fmt.Errorf("%w: starter agent name is required", domain.ErrInvalid))
			return
		}

		agentSlug = strings.TrimSpace(req.StarterAgent.Slug)
		if agentSlug == "" {
			RespondError(c, fmt.Errorf("%w: starter agent slug is required", domain.ErrInvalid))
			return
		}
		if err := domain.ValidateAgentSlug(agentSlug); err != nil {
			RespondError(c, err)
			return
		}

		agentRole = strings.TrimSpace(req.StarterAgent.Role)
		if agentRole == "" {
			RespondError(c, fmt.Errorf("%w: starter agent role is required", domain.ErrInvalid))
			return
		}

		agentBrief = strings.TrimSpace(req.StarterAgent.Brief)
		if agentBrief == "" {
			RespondError(c, fmt.Errorf("%w: starter agent brief is required", domain.ErrInvalid))
			return
		}

		agentModel = strings.TrimSpace(req.StarterAgent.Model)
		if agentModel == "" {
			RespondError(c, fmt.Errorf("%w: starter agent model is required", domain.ErrInvalid))
			return
		}

		if providerImpl != nil && providerImpl.RequiresMaxTokens() {
			if req.StarterAgent.MaxTokens == nil || *req.StarterAgent.MaxTokens <= 0 {
				RespondError(c, fmt.Errorf("%w: max_tokens is required for provider type %q", domain.ErrInvalid, pType))
				return
			}
		}
		if req.StarterAgent.MaxTokens != nil && *req.StarterAgent.MaxTokens <= 0 {
			RespondError(c, fmt.Errorf("%w: max_tokens must be positive", domain.ErrInvalid))
			return
		}

		if req.StarterAgent.Effort != nil && strings.TrimSpace(*req.StarterAgent.Effort) != "" {
			hint := services.EffectiveCatalogHint(pType, catalogProvider, baseURL)
			if err := validateEffort(c.Request.Context(), h.modelCatalog, pType, agentModel, hint, *req.StarterAgent.Effort); err != nil {
				RespondError(c, err)
				return
			}
		}

		if req.StarterAgent.Temperature != nil {
			if err := domain.ValidateAgentTemperature(*req.StarterAgent.Temperature); err != nil {
				RespondError(c, err)
				return
			}
			temp = *req.StarterAgent.Temperature
		}

		if req.StarterAgent.Autonomy != nil && *req.StarterAgent.Autonomy != "" {
			if err := domain.ValidateAgentAutonomy(*req.StarterAgent.Autonomy); err != nil {
				RespondError(c, err)
				return
			}
			autonomy = *req.StarterAgent.Autonomy
		}

		if len(req.StarterAgent.Avatar) > 0 {
			if err := domain.ValidateAgentAvatar(req.StarterAgent.Avatar); err != nil {
				RespondError(c, err)
				return
			}
			avatar = req.StarterAgent.Avatar
		}

		if req.StarterAgent.ContextWindow != nil {
			if err := domain.ValidateAgentContextWindow(req.StarterAgent.ContextWindow); err != nil {
				RespondError(c, err)
				return
			}
		}
	}

	var createdWs *domain.Workspace
	var ownerRole *domain.Role
	var creatorMember *domain.Member
	var createdProvider *domain.ProviderConfig
	var createdAgent *domain.Agent

	// Pre-create the starter agent's on-disk workspace directory with its base
	// prompt so a failure aborts the birth before any DB write.
	var starterAgentDir string
	if req.StarterAgent != nil {
		starterAgentDir = domain.AgentWorkspaceDir(h.workspaceDir, slug, agentSlug)
		if err := promptdocs.SeedWorkspace(starterAgentDir); err != nil {
			RespondError(c, fmt.Errorf("failed to create agent workspace directory: %w", err))
			return
		}
	}

	var desc string
	if req.Description != nil {
		desc = strings.TrimSpace(*req.Description)
	}

	err := h.store.WithTx(c.Request.Context(), func(txStore store.Store) error {
		ws := &domain.Workspace{
			Slug:        slug,
			Name:        name,
			Description: desc,
			Timezone:    tz,
			IsMaster:    false,
		}
		if err := txStore.Workspaces().Create(c.Request.Context(), ws); err != nil {
			return err
		}

		// Built-in Owner role
		oRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleOwner,
			IsOwner:     true,
			Permissions: domain.OwnerPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), oRole); err != nil {
			return err
		}

		// Built-in Admin role
		aRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleAdmin,
			IsOwner:     false,
			Permissions: domain.AdminPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), aRole); err != nil {
			return err
		}

		// Built-in Member role
		mRole := &domain.Role{
			WorkspaceID: ws.ID,
			Name:        domain.RoleMember,
			IsOwner:     false,
			Permissions: domain.MemberPermissions,
			BuiltIn:     true,
		}
		if err := txStore.Roles().Create(c.Request.Context(), mRole); err != nil {
			return err
		}

		// Assign creator as Owner
		member := &domain.Member{
			WorkspaceID: ws.ID,
			UserID:      user.ID,
			RoleID:      oRole.ID,
		}
		if err := txStore.Members().Add(c.Request.Context(), member); err != nil {
			return err
		}
		member.Role = oRole

		createdWs = ws
		ownerRole = oRole
		creatorMember = member

		// Atomic birth: provision provider and starter agent within the same transaction if requested
		if req.Provider != nil {
			var keyCiphertext, keyHint string
			if req.Provider.Key != nil && strings.TrimSpace(*req.Provider.Key) != "" {
				trimmedKey := strings.TrimSpace(*req.Provider.Key)
				envelope, err := secrets.Encrypt(h.encryptionKey, []byte(ws.ID), []byte(trimmedKey))
				if err != nil {
					return err
				}
				keyCiphertext = envelope
				keyHint = domain.GenerateKeyHint(trimmedKey)
			}

			prov := &domain.ProviderConfig{
				WorkspaceID:     ws.ID,
				Type:            pType,
				Name:            pName,
				BaseURL:         baseURL,
				CatalogProvider: catalogProvider,
				KeyCiphertext:   keyCiphertext,
				KeyHint:         keyHint,
				Enabled:         pEnabled,
			}
			if err := txStore.Providers().Create(c.Request.Context(), prov); err != nil {
				return err
			}
			createdProvider = prov

			if req.StarterAgent != nil {
				agentDisabledTools := req.StarterAgent.DisabledTools
				if agentDisabledTools == nil {
					agentDisabledTools = []string{}
				}
				enabledMCPS := req.StarterAgent.EnabledMCPS
				if enabledMCPS == nil {
					enabledMCPS = []string{}
				}
				var effort *string
				if req.StarterAgent.Effort != nil && strings.TrimSpace(*req.StarterAgent.Effort) != "" {
					eff := strings.TrimSpace(*req.StarterAgent.Effort)
					effort = &eff
				}
				var contextWindow *int = req.StarterAgent.ContextWindow
				if contextWindow == nil && h.modelCatalog != nil {
					hint := services.EffectiveCatalogHint(pType, catalogProvider, baseURL)
					contextWindow = h.modelCatalog.ResolveContextLimit(c.Request.Context(), pType, agentModel, hint)
				}
				agent := &domain.Agent{
					WorkspaceID:   ws.ID,
					Slug:          agentSlug,
					Name:          agentName,
					Role:          agentRole,
					Description:   strings.TrimSpace(req.StarterAgent.Description),
					Brief:         agentBrief,
					ProviderID:    prov.ID,
					Model:         agentModel,
					Temperature:   temp,
					MaxTokens:     req.StarterAgent.MaxTokens,
					Effort:        effort,
					Autonomy:      autonomy,
					ContextWindow: contextWindow,
					DisabledTools: agentDisabledTools,
					EnabledMCPS:   enabledMCPS,
					Avatar:        avatar,
					PromptsStatus: domain.PromptsStatusGenerating,
					CreatedBy:     &user.ID,
					UpdatedBy:     &user.ID,
				}
				if err := txStore.Agents().Create(c.Request.Context(), agent); err != nil {
					return err
				}
				createdAgent = agent
			}
		}

		return nil
	})

	if err != nil {
		// Clean up the pre-created directory on transaction failure
		if starterAgentDir != "" {
			_ = os.RemoveAll(starterAgentDir)
		}
		RespondError(c, err)
		return
	}

	// Post-commit: generate prompts synchronously for the starter agent
	// using the detached background context owned by the generation service.
	// Status transitions generating -> ready upon success; failure leaves
	// the workspace and agent created with status failed.
	if createdAgent != nil && h.agentService != nil {
		// Starter agents are always pinned (the payload requires provider and
		// model), so no workspace default rides the generation call.
		if err := h.agentService.Generate(context.Background(), starterAgentDir, createdWs.ID, createdAgent.ID, "", nil); err != nil {
			log.Printf("[handlers.workspaces] prompt generation failed for starter agent %s/%s: %v", createdWs.ID, createdAgent.ID, err)
		}

		// Refetch so the response reflects the final prompt state.
		if refreshed, err := h.store.Agents().ByID(c.Request.Context(), createdWs.ID, createdAgent.ID); err == nil {
			createdAgent = refreshed
		}
		composePromptDocuments(domain.AgentWorkspaceDir(h.workspaceDir, createdWs.Slug, createdAgent.Slug), createdAgent)
	}

	resp := gin.H{
		"workspace": createdWs,
		"role":      ownerRole,
		"member":    creatorMember,
	}
	if createdProvider != nil {
		resp["provider"] = toProviderResponse(createdProvider)
	}
	if createdAgent != nil {
		wrapped := newAgentResponseWith(c.Request.Context(), h.store.Providers(), h.modelCatalog, createdWs.ID, createdAgent, createdWs.DefaultModel)
		resp["starter_agent"] = wrapped
		resp["agent"] = wrapped
	}

	RespondCreated(c, resp)
}

// GetWorkspace returns details of the currently resolved workspace and current caller's role.
func (h *workspaceHandlers) GetWorkspace(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	role := MustCurrentRole(c)
	member := MustCurrentMember(c)

	RespondOK(c, gin.H{
		"workspace": ws,
		"role":      role,
		"my_role":   role,
		"member":    member,
	})
}

// PatchWorkspaceRequest holds editable fields for a workspace. DefaultModel
// follows the workspace default-model pair semantics (refactor-workspace-
// settings D2), decoded tri-state from the raw JSON: absent from the payload
// leaves the stored value untouched; the JSON literal null — like an explicit
// pair with both fields empty — is a clear attempt (subject to the
// inheriting-agents guard); a present pair with both fields set pins the
// default; a half-set pair is 400 invalid_request.
type PatchWorkspaceRequest struct {
	Name         *string         `json:"name"`
	Description  *string         `json:"description"`
	Timezone     *string         `json:"timezone"`
	DefaultModel json.RawMessage `json:"default_model"`
}

// PatchWorkspace updates name, description, timezone, and the default model of
// the current workspace. Slug remains immutable.
// The master workspace is editable only by superadmins: the master tenant has no role with
// workspace.write other than Superadmin, so the middleware permission guard enforces that rule.
func (h *workspaceHandlers) PatchWorkspace(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req PatchWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Description == nil && req.Timezone == nil && len(req.DefaultModel) == 0 {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: workspace name cannot be empty", domain.ErrInvalid))
			return
		}
		ws.Name = trimmed
	}

	if req.Description != nil {
		ws.Description = strings.TrimSpace(*req.Description)
	}

	if req.Timezone != nil {
		trimmed := strings.TrimSpace(*req.Timezone)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: timezone cannot be empty", domain.ErrInvalid))
			return
		}
		ws.Timezone = trimmed
	}

	if len(req.DefaultModel) > 0 {
		// Tri-state decode on the raw JSON: an explicit null unmarshals to a
		// nil pointer and falls through as the empty pair — a clear attempt —
		// while an absent key never enters this block.
		var decoded *domain.DefaultModelPair
		if err := json.Unmarshal(req.DefaultModel, &decoded); err != nil {
			RespondError(c, domain.ErrInvalid)
			return
		}
		pair := domain.DefaultModelPair{}
		if decoded != nil {
			pair = *decoded
		}
		pair.ProviderID = strings.TrimSpace(pair.ProviderID)
		pair.Model = strings.TrimSpace(pair.Model)
		if err := domain.ValidateDefaultModelPair(pair.ProviderID, pair.Model); err != nil {
			RespondError(c, err)
			return
		}
		if pair.ProviderID == "" {
			// Clearing is a cross-entity guard: agents carrying the empty pair
			// would lose their model at run start, so the clear is refused with
			// the inheriting count until they are re-pinned.
			inheriting, err := h.store.Agents().CountInheriting(c.Request.Context(), ws.ID)
			if err != nil {
				RespondError(c, err)
				return
			}
			if inheriting > 0 {
				RespondError(c, fmt.Errorf("%w: cannot clear the workspace default model: %d agent(s) inherit it — pin them to a provider first", domain.ErrUnprocessable, inheriting))
				return
			}
			ws.DefaultModel = nil
		} else {
			// The default must reference a provider config of this workspace;
			// the store's composite FK backs the check at the data layer.
			if _, err := h.store.Providers().ByID(c.Request.Context(), ws.ID, pair.ProviderID); err != nil {
				RespondError(c, fmt.Errorf("%w: default model provider not found in workspace", domain.ErrInvalid))
				return
			}
			ws.DefaultModel = &pair
		}
	}

	if err := h.store.Workspaces().Update(c.Request.Context(), ws); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"workspace": ws})
}
