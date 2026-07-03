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

func TestListProviders_Empty(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodGet, "/api/providers", "")
	w := httptest.NewRecorder()
	f.h.ListProviders(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCreateProvider_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/providers", "invalid-json")
	w := httptest.NewRecorder()
	f.h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateProvider_MissingName(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]string{"provider_type": "openai"})
	req := makeReq(http.MethodPost, "/api/providers", string(body))
	w := httptest.NewRecorder()
	f.h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateProvider_MissingProviderType(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]string{"name": "my-provider"})
	req := makeReq(http.MethodPost, "/api/providers", string(body))
	w := httptest.NewRecorder()
	f.h.CreateProvider(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateProvider_Success(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]interface{}{
		"name":          "openai-test",
		"provider_type": "openai",
		"enabled":       true,
	})
	req := makeReq(http.MethodPost, "/api/providers", string(body))
	w := httptest.NewRecorder()
	f.h.CreateProvider(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteProvider_NotFound(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodDelete, "/api/providers/ghost", "")
	w := httptest.NewRecorder()
	f.h.DeleteProvider(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSetSecret_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/providers/test/secret", "not-json")
	w := httptest.NewRecorder()
	f.h.SetSecret(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSetSecret_EmptyKey(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(map[string]string{"api_key": ""})
	req := makeReq(http.MethodPost, "/api/providers/test/secret", string(body))
	w := httptest.NewRecorder()
	f.h.SetSecret(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty API key, got %d", w.Code)
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/providers/{name}", f.h.GetProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/providers/ghost")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateProvider_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/providers/{name}", f.h.UpdateProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.NewRequest(http.MethodPut, server.URL+"/api/providers/openai-test", bytes.NewBufferString("not-json"))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(resp)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", res.StatusCode)
	}
}

func TestUpdateProvider_MissingProviderType(t *testing.T) {
	f := newHFixture(t)

	f.svc.CreateProvider(context.Background(), service.ProfileInput{
		Name:         "update-prov",
		ProviderType: "openai",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/providers/{name}", f.h.UpdateProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"api_base": "http://x"})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/providers/update-prov", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusOK {
		t.Errorf("expected 400 or 200, got %d", res.StatusCode)
	}
}

func TestUpdateProvider_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{
		Name:         "update-p",
		ProviderType: "openai",
	})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/providers/{name}", f.h.UpdateProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(service.ProfileInput{
		ProviderType: "anthropic",
		Enabled:      true,
	})
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/providers/update-p", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body2, _ := io.ReadAll(res.Body)
		t.Errorf("expected 200, got %d: %s", res.StatusCode, string(body2))
	}
}

func TestSetDefaultProvider_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/providers/{name}/default", f.h.SetDefaultProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/providers/ghost/default", nil)
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}

func TestSetDefaultProvider_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "def-p", ProviderType: "openai"})

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/providers/{name}/default", f.h.SetDefaultProvider)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/providers/def-p/default", nil)
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
}

func TestSetSecret_Success(t *testing.T) {
	f := newHFixture(t)
	ctx := context.Background()

	f.svc.CreateProvider(ctx, service.ProfileInput{Name: "key-prov", ProviderType: "openai"})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/providers/{name}/secret", f.h.SetSecret)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"api_key": "sk-1234567890abcdef"})
	res, err := http.Post(server.URL+"/api/providers/key-prov/secret", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Errorf("expected 200, got %d: %s", res.StatusCode, string(b))
	}
}

func TestGetSecretStatus_NotFound(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/providers/{name}/secret", f.h.GetSecretStatus)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/providers/ghost/secret")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
