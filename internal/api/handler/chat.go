package handler

import (
	"encoding/json"
	"net/http"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
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
	if req.PreviousResponseID != "" {
		ctx = middlewares.WithPreviousResponseID(ctx, req.PreviousResponseID)
	}

	it := assembledAgent.Run(ctx, req.Prompt, req.ContentBlocks...)
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

	if meta := assembledAgent.LastTurnMeta(); meta != nil {
		type turnSSEEvent struct {
			ConversationID     int64  `json:"conversation_id"`
			SequenceNum        int64  `json:"sequence_num"`
			ResponseID         string `json:"response_id"`
			PreviousResponseID string `json:"previous_response_id"`
			Model              string `json:"model"`
			Tokens             int64  `json:"tokens"`
		}
		_ = sse.WriteEvent("turn", turnSSEEvent{
			ConversationID:     meta.ConversationID,
			SequenceNum:        meta.SequenceNum,
			ResponseID:         meta.ResponseID,
			PreviousResponseID: meta.PreviousResponseID,
			Model:              meta.Model,
			Tokens:             meta.Tokens,
		})
	}

	_ = sse.WriteEvent("done", map[string]string{"status": "completed"})
}
