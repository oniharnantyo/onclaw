package handler

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListHooks handles GET /api/hooks.
func (h *Handler) ListHooks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListHooks(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// GetHook handles GET /api/hooks/{id}.
func (h *Handler) GetHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	resp, err := h.svc.GetHook(ctx, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// AddHook handles POST /api/hooks.
func (h *Handler) AddHook(w http.ResponseWriter, r *http.Request) {
	var input store.Hook
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Matcher != "" {
		if _, err := regexp.Compile(input.Matcher); err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid regex matcher: "+err.Error())
			return
		}
	}

	resp, err := h.svc.AddHook(r.Context(), &input)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// UpdateHook handles PUT /api/hooks/{id}.
func (h *Handler) UpdateHook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input store.Hook
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	input.ID = id

	if input.Matcher != "" {
		if _, err := regexp.Compile(input.Matcher); err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid regex matcher: "+err.Error())
			return
		}
	}

	resp, err := h.svc.UpdateHook(r.Context(), &input)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// RemoveHook handles DELETE /api/hooks/{id}.
func (h *Handler) RemoveHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	if err := h.svc.RemoveHook(ctx, id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ToggleHook handles POST /api/hooks/{id}/toggle.
func (h *Handler) ToggleHook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	type toggleInput struct {
		Enabled bool `json:"enabled"`
	}
	var input toggleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := h.svc.ToggleHook(ctx, id, input.Enabled); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// TestHook handles POST /api/hooks/test.
func (h *Handler) TestHook(w http.ResponseWriter, r *http.Request) {
	var input store.Hook
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Matcher != "" {
		if _, err := regexp.Compile(input.Matcher); err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid regex matcher: "+err.Error())
			return
		}
	}

	dec, err := h.svc.TestHook(r.Context(), &input)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]interface{}{
			"decision": dec,
			"error":    err.Error(),
		})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]interface{}{
		"decision": dec,
	})
}

// ListHookExecutions handles GET /api/hooks/executions.
func (h *Handler) ListHookExecutions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListHookExecutions(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
