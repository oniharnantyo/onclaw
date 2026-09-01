package handlers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// skillHandlers handles workspace skill management endpoints.
type skillHandlers struct {
	store store.Store
}

// NewSkillHandlers creates a new skillHandlers instance with injected dependencies.
func NewSkillHandlers(st store.Store) *skillHandlers {
	return &skillHandlers{
		store: st,
	}
}

// ListSkills lists all skills for the current workspace (progressive disclosure: omits body).
func (h *skillHandlers) ListSkills(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	skills, err := h.store.WorkspaceSkills().ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"skills": skills})
}

// GetSkill retrieves a single skill by ID including its full body.
func (h *skillHandlers) GetSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	skill, err := h.store.WorkspaceSkills().ByID(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"skill": skill})
}

// CreateSkillRequest holds the payload for creating a new workspace skill.
type CreateSkillRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// CreateSkill creates a new workspace skill document.
func (h *skillHandlers) CreateSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req CreateSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		RespondError(c, fmt.Errorf("%w: skill name cannot be empty", domain.ErrInvalid))
		return
	}

	if err := domain.ValidateSkillName(name); err != nil {
		RespondError(c, err)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	skill := &domain.WorkspaceSkill{
		WorkspaceID: ws.ID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Body:        req.Body,
		Enabled:     enabled,
	}

	if err := h.store.WorkspaceSkills().Create(c.Request.Context(), skill); err != nil {
		RespondError(c, err)
		return
	}

	RespondCreated(c, gin.H{"skill": skill})
}

// PatchSkillRequest holds fields for updating an existing workspace skill.
type PatchSkillRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Body        *string `json:"body,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

// PatchSkill updates an existing workspace skill.
func (h *skillHandlers) PatchSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	var req PatchSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if req.Name == nil && req.Description == nil && req.Body == nil && req.Enabled == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	existing, err := h.store.WorkspaceSkills().ByID(c.Request.Context(), ws.ID, id)
	if err != nil {
		RespondError(c, err)
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			RespondError(c, fmt.Errorf("%w: skill name cannot be empty", domain.ErrInvalid))
			return
		}
		if err := domain.ValidateSkillName(trimmed); err != nil {
			RespondError(c, err)
			return
		}
		existing.Name = trimmed
	}

	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	if req.Body != nil {
		existing.Body = *req.Body
	}

	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	if err := h.store.WorkspaceSkills().Update(c.Request.Context(), existing); err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"skill": existing})
}

// DeleteSkill deletes a workspace skill document.
func (h *skillHandlers) DeleteSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	id := c.Param("id")

	if err := h.store.WorkspaceSkills().Delete(c.Request.Context(), ws.ID, id); err != nil {
		RespondError(c, err)
		return
	}

	RespondNoContent(c)
}
