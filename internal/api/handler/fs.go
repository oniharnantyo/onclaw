package handler

import (
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

// BrowseFS handles GET /api/fs/browse
func (h *Handler) BrowseFS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path := r.URL.Query().Get("path")

	result, err := h.svc.BrowseFS(ctx, path)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, result)
}
