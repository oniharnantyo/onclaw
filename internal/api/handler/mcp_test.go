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
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestListMCP_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/mcp", "")
	w := httptest.NewRecorder()
	f.h.ListMCP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAddMCP_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/mcp", "not-json")
	w := httptest.NewRecorder()
	f.h.AddMCP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAddMCP_ValidationError(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]string{
		"name":      "bad-mcp",
		"transport": "grpc", // invalid
	})
	req := makeReq(http.MethodPost, "/api/mcp", string(body))
	w := httptest.NewRecorder()
	f.h.AddMCP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for validation error, got %d", w.Code)
	}
}

func TestAddMCP_Success(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.MCPServerInput{
		Name:      "my-mcp",
		Transport: "stdio",
		Command:   "npx foo",
	})
	req := makeReq(http.MethodPost, "/api/mcp", string(body))
	w := httptest.NewRecorder()
	f.h.AddMCP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMCP_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPut, "/api/mcp/test", "not-json")
	w := httptest.NewRecorder()
	f.h.UpdateMCP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRemoveMCP_NotFound(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodDelete, "/api/mcp/ghost", "")
	w := httptest.NewRecorder()
	f.h.RemoveMCP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestToggleMCPServer_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/mcp/foo/toggle", "not-json")
	w := httptest.NewRecorder()
	f.h.ToggleMCPServer(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTestMCP_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/mcp/test", "not-json")
	w := httptest.NewRecorder()
	f.h.TestMCP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTestMCP_Success(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.MCPServerInput{
		Name:      "test-mcp",
		Transport: "stdio",
		Command:   "cmd",
	})
	req := makeReq(http.MethodPost, "/api/mcp/test", string(body))
	w := httptest.NewRecorder()
	f.h.TestMCP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGetMCP_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/mcp/{name}", f.h.GetMCP)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/mcp/ghost")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateMCP_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/mcp/{name}", f.h.UpdateMCP)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(service.MCPServerInput{Transport: "stdio", Command: "cmd"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/mcp/ghost", bytes.NewBuffer(body))
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

func TestUpdateMCP_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.mcpStore.AddServer(ctx, &store.MCPServer{Name: "update-me", Transport: "stdio", Command: "old"})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/mcp/{name}", f.h.UpdateMCP)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(service.MCPServerInput{Transport: "stdio", Command: "new"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/mcp/update-me", bytes.NewBuffer(body))
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

func TestToggleMCPServer_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/mcp/{name}/toggle", f.h.ToggleMCPServer)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	res, err := http.Post(server.URL+"/api/mcp/ghost/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestToggleMCPServer_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()
	f.mcpStore.AddServer(ctx, &store.MCPServer{Name: "toggle-mcp", Transport: "stdio", Command: "cmd", Enabled: 1})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/mcp/{name}/toggle", f.h.ToggleMCPServer)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]bool{"enabled": false})
	res, err := http.Post(server.URL+"/api/mcp/toggle-mcp/toggle", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
}
