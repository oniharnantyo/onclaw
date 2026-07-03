package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/api/service"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestDiscoverSkills_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/skills/discover", "not-json")
	w := httptest.NewRecorder()
	f.h.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDiscoverSkills_MissingSource(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.DiscoverInput{Source: ""})
	req := makeReq(http.MethodPost, "/api/skills/discover", string(body))
	w := httptest.NewRecorder()
	f.h.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing source, got %d", w.Code)
	}
}

func TestInstallSkills_InvalidPayload(t *testing.T) {
	f := newHFixture(t)
	req := makeReq(http.MethodPost, "/api/skills", "not-json")
	w := httptest.NewRecorder()
	f.h.InstallSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestInstallSkills_MissingSource(t *testing.T) {
	f := newHFixture(t)
	body, _ := json.Marshal(service.InstallSkillInput{Source: ""})
	req := makeReq(http.MethodPost, "/api/skills", string(body))
	w := httptest.NewRecorder()
	f.h.InstallSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing source, got %d", w.Code)
	}
}

func TestListSkills_Error(t *testing.T) {
	t.Skip("ListSkills calls installer.List which requires a non-nil installer")
}

func TestGetSkill_NotFound(t *testing.T) {
	t.Skip("GetSkill requires a non-nil installer (skill.Installer)")
}

func TestUploadSkill_NoFile(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/skills/upload", f.h.UploadSkill)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/skills/upload", "application/octet-stream", bytes.NewBufferString("data"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUploadSkill_MultipartNoFile(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/skills/upload", f.h.UploadSkill)
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("scope", "global")
	w.Close()

	res, err := http.Post(server.URL+"/api/skills/upload", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing file field, got %d", res.StatusCode)
	}
}

func TestUploadSkill_InvalidType(t *testing.T) {
	f := newHFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/skills/upload", f.h.UploadSkill)
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	part.Write([]byte("hello"))
	w.Close()

	res, err := http.Post(server.URL+"/api/skills/upload", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid extension, got %d", res.StatusCode)
	}
}

func TestSkill_GetAndUpdateAndDelete(t *testing.T) {
	f := newHFixture(t)

	dummy := &store.Skill{
		Name:       "dummy",
		Scope:      "global",
		SourceType: "local",
	}
	_ = f.skillStore.AddSkill(context.Background(), dummy)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills/{name}", f.h.GetSkill)
	mux.HandleFunc("POST /api/skills/{name}/update", f.h.UpdateSkill)
	mux.HandleFunc("DELETE /api/skills/{name}", f.h.DeleteSkill)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/skills/dummy", nil)
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/skills/ghost", nil)
	res, err = (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/skills/dummy", nil)
	res, err = (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/skills/ghost", nil)
	res, err = (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
}
