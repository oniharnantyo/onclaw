package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestListTools_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/tools", "")
	w := httptest.NewRecorder()
	f.h.ListTools(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestToggleTool_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/tools/mytool/toggle", "not-json")
	w := httptest.NewRecorder()
	f.h.ToggleTool(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestPutCategoryConfig_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPut, "/api/tools/categories/Browser/config", "not-json")
	w := httptest.NewRecorder()
	f.h.PutCategoryConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestToggleTool_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.toolStore.UpsertTool(ctx, &store.ToolRegistry{Name: "ls", Category: "Filesystem", Enabled: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tools/{name}/toggle", f.h.ToggleTool)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	res, err := http.Post(server.URL+"/api/tools/ls/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
}

func TestToggleTool_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tools/{name}/toggle", f.h.ToggleTool)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": true})
	res, err := http.Post(server.URL+"/api/tools/ghost_tool/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent tool, got %d", res.StatusCode)
	}
}

func TestGetCategoryConfig_NotConfigurable(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tools/categories/{category}/config", f.h.GetCategoryConfig)
	server := httptest.NewServer(mux)
	defer server.Close()

	res, err := http.Get(server.URL + "/api/tools/categories/FakeCategory/config")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-configurable category, got %d", res.StatusCode)
	}
}
