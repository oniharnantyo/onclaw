package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestListAgents_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/agents", "")
	w := httptest.NewRecorder()
	f.h.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCreateAgent_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/agents", "not-json")
	w := httptest.NewRecorder()
	f.h.CreateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateAgent_MissingName(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]string{"provider": "openai"})
	req := makeReq(http.MethodPost, "/api/agents", string(body))
	w := httptest.NewRecorder()
	f.h.CreateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateAgent_Success(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.AgentInput{
		Name:     "my-agent",
		Provider: "openai",
		Model:    "gpt-4",
	})
	req := makeReq(http.MethodPost, "/api/agents", string(body))
	w := httptest.NewRecorder()
	f.h.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPut, "/api/agents/test", "not-json")
	w := httptest.NewRecorder()
	f.h.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodDelete, "/api/agents/ghost", "")
	w := httptest.NewRecorder()
	f.h.DeleteAgent(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUpdateAgent_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{name}", f.h.UpdateAgent)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(service.AgentInput{Provider: "openai"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/agents/ghost", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestUpdateAgent_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.svc.CreateAgent(ctx, service.AgentInput{Name: "upd-agt", Provider: "openai"})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{name}", f.h.UpdateAgent)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(service.AgentInput{Provider: "anthropic", Model: "claude-3"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/agents/upd-agt", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Errorf("expected 200, got %d: %s", res.StatusCode, string(b))
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents/{name}", f.h.GetAgent)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/agents/ghost")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
