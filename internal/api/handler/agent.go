package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListAgents handles listing of all agent profiles.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListAgents(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// CreateAgent handles creating a new agent profile.
func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var input service.AgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Agent name is required")
		return
	}

	a, err := h.svc.CreateAgent(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, a)
}

// GetAgent handles retrieving a single agent profile.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	resp, err := h.svc.GetAgent(ctx, name)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// UpdateAgent handles updating an existing agent profile.
func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var input service.AgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	a, err := h.svc.UpdateAgent(ctx, name, input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, a)
}

// DeleteAgent handles removing an agent profile.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	if err := h.svc.DeleteAgent(ctx, name); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
