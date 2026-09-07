package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// roleHandlers handles role listing endpoints.
type roleHandlers struct {
	roles store.RoleStore
}

// NewRoleHandlers creates a new roleHandlers instance with injected dependencies.
func NewRoleHandlers(roles store.RoleStore) *roleHandlers {
	return &roleHandlers{
		roles: roles,
	}
}

// ListRoles lists all roles available in the current workspace.
func (h *roleHandlers) ListRoles(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	roles, err := h.roles.ListForWorkspace(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, gin.H{"roles": roles})
}
