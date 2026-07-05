package handler

import (
	"net/http"
	"strconv"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
)

// ListDreamSweeps handles GET /api/memory/dreams.
func (h *Handler) ListDreamSweeps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListDreamSweeps(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// ListStagedWrites handles GET /api/memory/staged.
func (h *Handler) ListStagedWrites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListStagedWrites(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// ApproveStagedWrite handles POST /api/memory/staged/{id}/approve.
func (h *Handler) ApproveStagedWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid staged write id")
		return
	}
	if err := h.svc.ApproveStagedWrite(ctx, id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// RejectStagedWrite handles POST /api/memory/staged/{id}/reject.
func (h *Handler) RejectStagedWrite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid staged write id")
		return
	}
	if err := h.svc.RejectStagedWrite(ctx, id); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
