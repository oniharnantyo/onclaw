package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListTools handles GET /api/tools.
func (h *Handler) ListTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListTools(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// ToggleTool handles POST /api/tools/{name}/toggle.
func (h *Handler) ToggleTool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var input service.ToggleToolInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.ToggleTool(ctx, name, input.Enabled); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// GetCategoryConfig handles GET /api/tools/categories/{cat}/config.
func (h *Handler) GetCategoryConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat := r.PathValue("cat")

	resp, err := h.svc.GetCategoryConfig(ctx, cat)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// PutCategoryConfig handles PUT /api/tools/categories/{cat}/config.
func (h *Handler) PutCategoryConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cat := r.PathValue("cat")

	var input service.PutCategoryConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.PutCategoryConfig(ctx, cat, input.Config); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
