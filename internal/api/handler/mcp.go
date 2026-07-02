package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListMCP handles GET /api/mcp.
func (h *Handler) ListMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListMCP(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// GetMCP handles GET /api/mcp/{name}.
func (h *Handler) GetMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	resp, err := h.svc.GetMCP(ctx, name)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// AddMCP handles POST /api/mcp.
func (h *Handler) AddMCP(w http.ResponseWriter, r *http.Request) {
	var input service.MCPServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	resp, err := h.svc.AddMCP(r.Context(), &input)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// UpdateMCP handles PUT /api/mcp/{name}.
func (h *Handler) UpdateMCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var input service.MCPServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	resp, err := h.svc.UpdateMCP(r.Context(), name, &input)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// RemoveMCP handles DELETE /api/mcp/{name}.
func (h *Handler) RemoveMCP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	if err := h.svc.RemoveMCP(ctx, name); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ToggleMCPServer handles POST /api/mcp/{name}/toggle.
func (h *Handler) ToggleMCPServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var input service.ToggleMCPServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.ToggleMCPServer(ctx, name, input.Enabled); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// TestMCP handles POST /api/mcp/test.
func (h *Handler) TestMCP(w http.ResponseWriter, r *http.Request) {
	var input service.MCPServerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	toolNames, err := h.svc.TestMCP(r.Context(), &input)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]interface{}{
			"tools": []string{},
			"error": err.Error(),
		})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]interface{}{
		"tools": toolNames,
	})
}
