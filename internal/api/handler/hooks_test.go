package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestListHooks_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/hooks", "")
	w := httptest.NewRecorder()
	f.h.ListHooks(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAddHook_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/hooks", "not-json")
	w := httptest.NewRecorder()
	f.h.AddHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAddHook_InvalidMatcher(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(store.Hook{
		Name:    "bad-matcher",
		Matcher: "[invalid-regex",
	})
	req := makeReq(http.MethodPost, "/api/hooks", string(body))
	w := httptest.NewRecorder()
	f.h.AddHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid matcher, got %d", w.Code)
	}
}

func TestAddHook_Success(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(store.Hook{
		Name:        "my-hook",
		Event:       "pre_tool_use",
		HandlerType: "command",
		Matcher:     "list_.*",
	})
	req := makeReq(http.MethodPost, "/api/hooks", string(body))
	w := httptest.NewRecorder()
	f.h.AddHook(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateHook_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPut, "/api/hooks/h1", "not-json")
	w := httptest.NewRecorder()
	f.h.UpdateHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRemoveHook_NotFound(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodDelete, "/api/hooks/ghost", "")
	w := httptest.NewRecorder()
	f.h.RemoveHook(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestToggleHook_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/hooks/h1/toggle", "not-json")
	w := httptest.NewRecorder()
	f.h.ToggleHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListHookExecutions_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/hooks/executions", "")
	w := httptest.NewRecorder()
	f.h.ListHookExecutions(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestTestHook_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/hooks/test", "not-json")
	w := httptest.NewRecorder()
	f.h.TestHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTestHook_InvalidMatcher(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(store.Hook{Matcher: "[bad"})
	req := makeReq(http.MethodPost, "/api/hooks/test", string(body))
	w := httptest.NewRecorder()
	f.h.TestHook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid matcher, got %d", w.Code)
	}
}

func TestGetHook_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/hooks/{id}", f.h.GetHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/hooks/ghost")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateHook_UpsertBehavior(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/hooks/{id}", f.h.UpdateHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(store.Hook{Name: "updated"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/hooks/ghost", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (upsert), got %d", res.StatusCode)
	}
}

func TestUpdateHook_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.hookStore.AddHook(ctx, &store.Hook{ID: "h-upd", Name: "old"})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/hooks/{id}", f.h.UpdateHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(store.Hook{Name: "new-name"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/hooks/h-upd", bytes.NewBuffer(body))
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

func TestToggleHook_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()
	f.hookStore.AddHook(ctx, &store.Hook{ID: "tg-h", Enabled: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hooks/{id}/toggle", f.h.ToggleHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	res, err := http.Post(server.URL+"/api/hooks/tg-h/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
}

func TestToggleHook_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hooks/{id}/toggle", f.h.ToggleHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	res, err := http.Post(server.URL+"/api/hooks/ghost/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestTestHook_Success(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/hooks/test", f.h.TestHook)
	server := httptest.NewServer(mux)
	defer server.Close()

	h := store.Hook{
		Name:        "test-hook",
		Event:       "pre_tool_use",
		HandlerType: "command",
		Config:      `{"command":"echo hello"}`,
	}
	body, _ := json.Marshal(h)
	res, err := http.Post(server.URL+"/api/hooks/test", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusUnprocessableEntity {
		b, _ := io.ReadAll(res.Body)
		t.Errorf("expected 200 or 422, got %d: %s", res.StatusCode, string(b))
	}
}
