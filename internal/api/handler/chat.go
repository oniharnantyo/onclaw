package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

type chatInitEvent struct {
	ConversationID int64 `json:"conversation_id"`
}

// Chat handles agent chat requests using Server-Sent Events (SSE).
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req service.ChatInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Prompt == "" {
		httpx.Error(w, http.StatusBadRequest, "Prompt is required")
		return
	}

	convID, assembledAgent, err := h.svc.Chat(ctx, req)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Initialize SSE
	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Send init event to inform client of the conversation ID
	if err := sse.WriteEvent("init", chatInitEvent{ConversationID: convID}); err != nil {
		return
	}

	// Run the agent iteration
	it := assembledAgent.Run(ctx, req.Prompt)
	for {
		msg, ok := it.Next()
		if !ok {
			break
		}
		if err := sse.WriteEvent("message", msg); err != nil {
			return
		}
	}

	if err := it.Err(); err != nil {
		_ = sse.WriteEvent("error", map[string]string{"error": err.Error()})
		return
	}

	_ = sse.WriteEvent("done", map[string]string{"status": "completed"})
}
