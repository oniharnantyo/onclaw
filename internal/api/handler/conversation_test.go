package handler_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}
