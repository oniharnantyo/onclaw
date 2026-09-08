package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// CodeMemoryCapExceeded marks 422 memory-cap rejections in the error envelope.
const CodeMemoryCapExceeded = "memory_cap_exceeded"

// memoryResponse is the payload shape for both human memory endpoints:
// clients hold zero constants — max_chars always rides along.
type memoryResponse struct {
	Content   string     `json:"content"`
	MaxChars  int        `json:"max_chars"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// newMemoryResponse renders a store memory (nil = nothing stored yet → empty
// document with a null timestamp).
func newMemoryResponse(mem *domain.Memory) memoryResponse {
	if mem == nil {
		return memoryResponse{MaxChars: domain.MaxMemoryContentChars}
	}
	return memoryResponse{
		Content:   mem.Content,
		MaxChars:  domain.MaxMemoryContentChars,
		UpdatedAt: &mem.UpdatedAt,
	}
}

// memoryHandlers serves the human edit surfaces for shared memory scopes
// (design D8): own user memory for any member, workspace memory read for
// members and written under the workspace settings-management permission.
type memoryHandlers struct {
	memories store.MemoryStore
}

// NewMemoryHandlers creates a new memoryHandlers instance with injected dependencies.
func NewMemoryHandlers(memories store.MemoryStore) *memoryHandlers {
	return &memoryHandlers{memories: memories}
}

// memoryPutRequest is the body for both PUT endpoints.
type memoryPutRequest struct {
	Content string `json:"content"`
}

// respondMemoryCap writes the 422 envelope for over-cap content: the message
// names the limit (shared domain error) and the details carry max_chars for UI
// counters.
func respondMemoryCap(c *gin.Context, err error) {
	AbortWithError(c, http.StatusUnprocessableEntity, CodeMemoryCapExceeded, err.Error(),
		ErrorDetail{Field: "max_chars", Message: strconv.Itoa(domain.MaxMemoryContentChars)})
}

// GetUserMemory returns the authenticated member's own user memory for the
// workspace. The user identity comes from the auth context — self-scoped by
// construction, no parameter can address another user.
func (h *memoryHandlers) GetUserMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	// Absence is a normal state: the store returns (nil, nil).
	mem, err := h.memories.UserMemory(c.Request.Context(), ws.ID, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, newMemoryResponse(mem))
}

// PutUserMemory replaces the authenticated member's own user memory.
func (h *memoryHandlers) PutUserMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	user := MustCurrentUser(c)

	var req memoryPutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if err := domain.ValidateMemoryContent(req.Content); err != nil {
		if errors.Is(err, domain.ErrMemoryCapExceeded) {
			respondMemoryCap(c, err)
			return
		}
		RespondError(c, err)
		return
	}

	if err := h.memories.UpsertUserMemory(c.Request.Context(), ws.ID, user.ID, req.Content); err != nil {
		RespondError(c, err)
		return
	}

	// Refetch so the response carries the store-assigned timestamp.
	saved, err := h.memories.UserMemory(c.Request.Context(), ws.ID, user.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, newMemoryResponse(saved))
}

// GetWorkspaceMemory returns the shared workspace memory. Any member may read.
func (h *memoryHandlers) GetWorkspaceMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	mem, err := h.memories.WorkspaceMemory(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, newMemoryResponse(mem))
}

// PutWorkspaceMemory replaces the shared workspace memory. Route-level
// middleware gates it on the workspace settings-management permission (the
// same workspace.write gate as the workspace PATCH); the store persists via a
// targeted update so no other workspace field can be touched.
func (h *memoryHandlers) PutWorkspaceMemory(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	var req memoryPutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	if err := domain.ValidateMemoryContent(req.Content); err != nil {
		if errors.Is(err, domain.ErrMemoryCapExceeded) {
			respondMemoryCap(c, err)
			return
		}
		RespondError(c, err)
		return
	}

	if err := h.memories.UpsertWorkspaceMemory(c.Request.Context(), ws.ID, req.Content); err != nil {
		RespondError(c, err)
		return
	}

	// Refetch so the response carries the store-assigned timestamp.
	saved, err := h.memories.WorkspaceMemory(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	RespondOK(c, newMemoryResponse(saved))
}
