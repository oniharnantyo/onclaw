package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/agent/middlewares"
	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/conversation"
)

type chatInitEvent struct {
	ConversationID int64  `json:"conversation_id"`
	ContextWindow  int64  `json:"context_window"`
	AgentName      string `json:"agent_name"`
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

	convID, assembledAgent, sessionMgr, err := h.svc.Chat(ctx, req)
	if err != nil {
		// The input-safety floor guard fails fast before any model call; the
		// client can fix the agent's tool set, so this is a 400 not a 500.
		if errors.Is(err, middlewares.ErrInputFloorExceedsSafetyLimit) {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
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
	if err := sse.WriteEvent("init", chatInitEvent{
		ConversationID: convID,
		ContextWindow:  int64(assembledAgent.ContextWindow()),
		AgentName:      assembledAgent.AgentName(),
	}); err != nil {
		return
	}

	// Run the agent iteration
	if req.PreviousResponseID != "" {
		ctx = conversation.WithPreviousResponseID(ctx, req.PreviousResponseID)
	}
	ctx = middlewares.WithStreaming(ctx, true)

	history, _, err := sessionMgr.LoadHistory(ctx)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	userMsg := schema.UserAgenticMessage(req.Prompt)
	for _, cb := range req.ContentBlocks {
		if cb != nil {
			userMsg.ContentBlocks = append(userMsg.ContentBlocks, cb)
		}
	}
	turnMsgs := append(history, userMsg)

	it := assembledAgent.Run(ctx, turnMsgs)

	// Trace premature request-context cancellation (client/proxy teardown while
	// the turn is still running). r.Context() is also cancelled on normal
	// completion, so `finished` is closed on return and we only warn when the
	// cancel arrived before the turn completed — the signature of a disconnect
	// leaking into tool execution as context.Canceled.
	finished := make(chan struct{})
	defer close(finished)
	go func(convID int64) {
		<-ctx.Done()
		select {
		case <-finished:
			// expected: turn completed, then the request context was cancelled
		default:
			slog.Warn("chat_context_cancelled_premature",
				"conversation_id", convID,
				"err", ctx.Err(),
			)
		}
	}(convID)

	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		// Summarization compaction progress: emit progress before status so the
		// frontend's independent banner/bar state renders correctly at both the
		// start (bar set, then banner revealed) and end (bar fills, then hides).
		if ev.Compaction != nil {
			_ = sse.WriteEvent("compaction_progress", map[string]int{"progress": ev.Compaction.Progress})
			if ev.Compaction.Status != "" {
				_ = sse.WriteEvent("compaction", map[string]string{"status": string(ev.Compaction.Status)})
			}
			continue
		}
		if err := sse.WriteEvent("message", ev.Message); err != nil {
			slog.Warn("chat_stream_write_failed",
				"conversation_id", convID,
				"err", err,
			)
			return
		}
	}

	if err := it.Err(); err != nil {
		slog.Warn("chat_turn_error",
			"conversation_id", convID,
			"err", err,
		)
		_ = sse.WriteEvent("error", map[string]string{"error": err.Error()})
		return
	}

	// CollectedTurn() carries only agent outputs (assistant/tool messages);
	// the driving user prompt is input, never echoed by the iterator. Prepend it
	// so the persisted turn and the `question` column include the user query.
	turnWithUser := append([]*schema.AgenticMessage{userMsg}, it.CollectedTurn()...)
	meta, err := sessionMgr.CommitTurn(ctx, turnWithUser)
	if err != nil {
		slog.Warn("chat_commit_turn_failed",
			"conversation_id", convID,
			"err", err,
		)
		_ = sse.WriteEvent("error", map[string]string{"error": fmt.Sprintf("commit turn failed: %v", err)})
		return
	}

	if meta != nil {
		type usageSSEEvent struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		}
		_ = sse.WriteEvent("usage", usageSSEEvent{
			PromptTokens:     meta.PromptTokens,
			CompletionTokens: meta.CompletionTokens,
			TotalTokens:      meta.Tokens,
		})

		type turnSSEEvent struct {
			ConversationID     int64  `json:"conversation_id"`
			SequenceNum        int64  `json:"sequence_num"`
			ResponseID         string `json:"response_id"`
			PreviousResponseID string `json:"previous_response_id"`
			Model              string `json:"model"`
			Tokens             int64  `json:"tokens"`
			PromptTokens       int64  `json:"prompt_tokens"`
			CompletionTokens   int64  `json:"completion_tokens"`
			TotalTokens        int64  `json:"total_tokens"`
		}
		_ = sse.WriteEvent("turn", turnSSEEvent{
			ConversationID:     meta.ConversationID,
			SequenceNum:        meta.SequenceNum,
			ResponseID:         meta.ResponseID,
			PreviousResponseID: meta.PreviousResponseID,
			Model:              meta.Model,
			Tokens:             meta.Tokens,
			PromptTokens:       meta.PromptTokens,
			CompletionTokens:   meta.CompletionTokens,
			TotalTokens:        meta.Tokens,
		})
	}

	_ = sse.WriteEvent("done", map[string]string{"status": "completed"})
}
