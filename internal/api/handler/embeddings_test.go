package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
)

func TestEmbeddingsConfig_GetAndSet(t *testing.T) {
	f := newHFixture(t)

	// 1. Initial GET -> empty defaults
	reqGet := makeReq(http.MethodGet, "/api/config/embeddings", "")
	wGet := httptest.NewRecorder()
	f.h.GetEmbeddingsConfig(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wGet.Code)
	}
	var initCfg service.EmbeddingsConfig
	if err := json.Unmarshal(wGet.Body.Bytes(), &initCfg); err != nil {
		t.Fatalf("unmarshal init response: %v", err)
	}

	// 2. PUT -> set values
	body := `{"provider":"openai","model":"text-embedding-3-small","api_base":"https://api.openai.com/v1","timeout":"45s"}`
	reqPut := makeReq(http.MethodPut, "/api/config/embeddings", body)
	wPut := httptest.NewRecorder()
	f.h.SetEmbeddingsConfig(wPut, reqPut)
	if wPut.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wPut.Code)
	}

	// 3. GET -> verify set values persist
	reqGet2 := makeReq(http.MethodGet, "/api/config/embeddings", "")
	wGet2 := httptest.NewRecorder()
	f.h.GetEmbeddingsConfig(wGet2, reqGet2)
	if wGet2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wGet2.Code)
	}
	var updatedCfg service.EmbeddingsConfig
	if err := json.Unmarshal(wGet2.Body.Bytes(), &updatedCfg); err != nil {
		t.Fatalf("unmarshal updated response: %v", err)
	}
	if updatedCfg.Provider != "openai" || updatedCfg.Model != "text-embedding-3-small" || updatedCfg.APIBase != "https://api.openai.com/v1" || updatedCfg.Timeout != "45s" {
		t.Errorf("unexpected updated embeddings config: %+v", updatedCfg)
	}
}

func TestEmbeddingsConfig_Set_InvalidJSON(t *testing.T) {
	f := newHFixture(t)
	reqPut := makeReq(http.MethodPut, "/api/config/embeddings", "{invalid-json")
	wPut := httptest.NewRecorder()
	f.h.SetEmbeddingsConfig(wPut, reqPut)
	if wPut.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid json, got %d", wPut.Code)
	}
}
