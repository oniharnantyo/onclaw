package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// GetEmbeddingsConfig handles GET /api/config/embeddings.
func (h *Handler) GetEmbeddingsConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := h.svc.GetEmbeddingsConfig(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cfg)
}

// SetEmbeddingsConfig handles PUT /api/config/embeddings.
func (h *Handler) SetEmbeddingsConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var cfg service.EmbeddingsConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.svc.SetEmbeddingsConfig(ctx, &cfg); err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cfg)
}
