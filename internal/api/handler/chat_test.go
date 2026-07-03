package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestChat_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/chat", "bad-json")
	w := httptest.NewRecorder()
	f.h.Chat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChat_EmptyPrompt(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.ChatInput{Prompt: ""})
	req := makeReq(http.MethodPost, "/api/chat", string(body))
	w := httptest.NewRecorder()
	f.h.Chat(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
