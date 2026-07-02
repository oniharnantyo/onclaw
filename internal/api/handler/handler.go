package handler

import (
	"errors"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// Handler holds the service dependency and handles HTTP request-response translations.
type Handler struct {
	svc *service.Service
}

// New returns a new Handler instance.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) handleError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, service.ErrInvalidInput) {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.Error(w, http.StatusInternalServerError, err.Error())
}
