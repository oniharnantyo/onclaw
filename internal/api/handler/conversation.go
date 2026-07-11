package handler

import (
	"net/http"
	"strconv"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// ListConversations handles listing of all conversations.
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.svc.ListConversations(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, list)
}

// ListMessages handles listing messages within a single conversation.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid conversation ID")
		return
	}

	messages, contextWindow, err := h.svc.ListMessages(ctx, id)
	if err != nil {
		h.handleError(w, err)
		return
	}

	type listMessagesResponse struct {
		Messages      []*store.TurnRow `json:"messages"`
		ContextWindow int64            `json:"context_window"`
	}

	httpx.JSON(w, http.StatusOK, listMessagesResponse{
		Messages:      messages,
		ContextWindow: contextWindow,
	})
}
