package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestListConversations(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/conversations", "")
	w := httptest.NewRecorder()
	f.h.ListConversations(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListMessages_InvalidID(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/conversations/not-a-number/messages", "")
	req = req.WithContext(req.Context())
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/conversations/{id}/messages", f.h.ListMessages)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/conversations/not-a-number/messages")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid ID, got %d", resp.StatusCode)
	}
}

func TestListMessages_ValidID(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/conversations/{id}/messages", f.h.ListMessages)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/conversations/1/messages")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var wrapper struct {
		Messages      []*store.TurnRow `json:"messages"`
		ContextWindow int64            `json:"context_window"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if wrapper.ContextWindow <= 0 {
		t.Errorf("expected context_window > 0, got %d", wrapper.ContextWindow)
	}
}
