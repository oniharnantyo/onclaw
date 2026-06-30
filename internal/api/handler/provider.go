package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListProviders handles listing of all provider profiles.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListProviders(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// CreateProvider handles creation of a provider profile.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var input service.ProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "Provider name is required")
		return
	}
	if input.ProviderType == "" {
		httpx.Error(w, http.StatusBadRequest, "Provider type is required")
		return
	}

	p, err := h.svc.CreateProvider(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, p)
}

// GetProvider handles retrieving a single provider profile.
func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	resp, err := h.svc.GetProvider(ctx, name)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// UpdateProvider handles updating an existing provider profile.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var input service.ProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	p, err := h.svc.UpdateProvider(ctx, name, input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, p)
}

// DeleteProvider handles removing a provider profile.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	if err := h.svc.DeleteProvider(ctx, name); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// SetDefaultProvider sets the default provider preference.
func (h *Handler) SetDefaultProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	if err := h.svc.SetDefaultProvider(ctx, name); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "default_set"})
}

// GetSecretStatus returns whether the API key secret is set and its hint.
func (h *Handler) GetSecretStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	resp, err := h.svc.GetSecretStatus(ctx, name)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// SetSecret sets the provider API key secret.
func (h *Handler) SetSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")

	var req service.SetSecretInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.APIKey == "" {
		httpx.Error(w, http.StatusBadRequest, "API key cannot be empty")
		return
	}

	if err := h.svc.SetSecret(ctx, name, req.APIKey); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "secret_set"})
}
