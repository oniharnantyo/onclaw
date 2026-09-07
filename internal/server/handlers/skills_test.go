package handlers_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/server"
	"github.com/oniharnantyo/onclaw/internal/server/handlers"
	"github.com/oniharnantyo/onclaw/internal/skills"
	"github.com/oniharnantyo/onclaw/internal/store"
	storefake "github.com/oniharnantyo/onclaw/internal/store/fake"
)

// newSkillsTestEnv builds a gin engine with the skill handlers wired over the
// fake store and a temp OnClaw dir, mirroring the composition root's adapter
// wiring (registry store -> skills.Store -> install service).
func newSkillsTestEnv(t *testing.T) (*gin.Engine, store.Store, *domain.Workspace, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	st := storefake.New()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(nil, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	onClawDir := t.TempDir()
	install := skills.NewInstallService(
		server.WorkspaceSkillsInstallStore(st.WorkspaceSkills()),
		st.Agents(),
		st.ToolSettings(),
	)
	h := handlers.NewSkillHandlers(install, st.WorkspaceSkills(), st.Agents(), onClawDir)

	r := gin.New()
	group := r.Group("/api/v1/workspaces/:ws")
	group.Use(func(c *gin.Context) {
		resolved, err := st.Workspaces().BySlug(nil, c.Param("ws"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.Set(handlers.WorkspaceContextKey, resolved)
		c.Next()
	})
	group.GET("/skills", h.ListSkills)
	group.POST("/skills", h.CreateSkill)
	group.POST("/skills/inspect", h.InspectUpload)
	group.POST("/skills/inspect/git", h.InspectGit)
	group.GET("/skills/:name", h.GetSkill)
	group.PUT("/skills/:name", h.UpdateSkill)
	group.PATCH("/skills/:name", h.PatchSkill)
	group.DELETE("/skills/:name", h.DeleteSkill)
	group.POST("/skills/:name/dependencies/recheck", h.RecheckSkill)
	group.GET("/agents/:agent/skills", h.ListAgentSkills)
	group.POST("/agents/:agent/skills", h.InstallAgentSkill)
	group.DELETE("/agents/:agent/skills/:name", h.RemoveAgentSkill)
	return r, st, ws, onClawDir
}

func doJSON(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestSkills_ListIncludesSystemTier(t *testing.T) {
	r, st, ws, _ := newSkillsTestEnv(t)

	row := &domain.WorkspaceSkill{
		WorkspaceID: ws.ID,
		Name:        "changelog-sweeper",
		Description: "sweeps changelogs",
		Version:     "0.1.0",
		Source:      domain.SkillSourceAuthored,
		Enabled:     true,
	}
	if err := st.WorkspaceSkills().Create(nil, row); err != nil {
		t.Fatalf("seed skill row: %v", err)
	}

	rec := doJSON(r, http.MethodGet, "/api/v1/workspaces/acme/skills", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Skills []struct {
			Name   string `json:"name"`
			Tier   string `json:"tier"`
			Locked bool   `json:"locked"`
			Source string `json:"source"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	foundWorkspace, foundSystem := false, false
	for _, sk := range body.Skills {
		if sk.Name == "changelog-sweeper" && sk.Tier == "workspace" && !sk.Locked {
			foundWorkspace = true
		}
		if sk.Name == "web-research" && sk.Tier == "system" && sk.Locked && sk.Source == "system" {
			foundSystem = true
		}
	}
	if !foundWorkspace {
		t.Error("expected the seeded workspace-tier skill in the list")
	}
	if !foundSystem {
		t.Error("expected the locked system tier (web-research) in the list")
	}
}

func TestSkills_AuthorInstallLifecycle(t *testing.T) {
	r, _, _, onClawDir := newSkillsTestEnv(t)

	// Install.
	rec := doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/skills", map[string]any{
		"source":      "authored",
		"name":        "Changelog Sweeper",
		"description": "sweeps changelogs",
		"body":        "Sweep the changelog for releases.",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("install: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Skill struct {
			Name    string `json:"name"`
			Tier    string `json:"tier"`
			Version string `json:"version"`
			Source  string `json:"source"`
			Enabled bool   `json:"enabled"`
		} `json:"skill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Skill.Name != "changelog-sweeper" || created.Skill.Tier != "workspace" ||
		created.Skill.Source != "authored" || created.Skill.Version != "0.1.0" || !created.Skill.Enabled {
		t.Errorf("unexpected install response: %+v", created.Skill)
	}

	skillDir := filepath.Join(onClawDir, "workspaces", "acme", "skills", "changelog-sweeper")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("expected SKILL.md on disk: %v", err)
	}

	// Detail includes the body.
	rec = doJSON(r, http.MethodGet, "/api/v1/workspaces/acme/skills/changelog-sweeper", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"body"`)) {
		t.Errorf("expected body in detail response: %s", rec.Body.String())
	}

	// Master switch off.
	rec = doJSON(r, http.MethodPatch, "/api/v1/workspaces/acme/skills/changelog-sweeper", map[string]any{"enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Errorf("expected enabled false in patch response: %s", rec.Body.String())
	}

	// Uninstall removes row and tree.
	rec = doJSON(r, http.MethodDelete, "/api/v1/workspaces/acme/skills/changelog-sweeper", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf("expected skill directory removed, stat err=%v", err)
	}
	rec = doJSON(r, http.MethodGet, "/api/v1/workspaces/acme/skills/changelog-sweeper", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after uninstall, got %d", rec.Code)
	}
}

func TestSkills_CreateConflictWithoutOverwrite(t *testing.T) {
	r, _, _, _ := newSkillsTestEnv(t)

	payload := map[string]any{"source": "authored", "name": "dup-skill", "body": "body one"}
	if rec := doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/skills", payload); rec.Code != http.StatusCreated {
		t.Fatalf("first install: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec := doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/skills", payload)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second install: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	payload["overwrite"] = true
	if rec := doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/skills", payload); rec.Code != http.StatusCreated {
		t.Fatalf("overwrite install: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSkills_PatchValidation(t *testing.T) {
	r, _, _, _ := newSkillsTestEnv(t)

	rec := doJSON(r, http.MethodPatch, "/api/v1/workspaces/acme/skills/missing", map[string]any{"enabled": true})
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	rec = doJSON(r, http.MethodPatch, "/api/v1/workspaces/acme/skills/missing", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing enabled, got %d", rec.Code)
	}
}

// zipBytes builds an in-memory zip archive from name->content entries.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// postMultipartArchive sends an archive file under the `archive` field.
func postMultipartArchive(r *gin.Engine, path string, field string, fileName string, archive []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile(field, fileName)
	_, _ = fw.Write(archive)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestSkills_InspectUpload(t *testing.T) {
	r, _, _, _ := newSkillsTestEnv(t)

	post := func(archive []byte) *httptest.ResponseRecorder {
		return postMultipartArchive(r, "/api/v1/workspaces/acme/skills/inspect", "archive", "skill.zip", archive)
	}

	good := zipBytes(t, map[string]string{
		"SKILL.md":       "---\nname: pdf-tidier\ndescription: tidies PDFs\n---\nTidy PDFs.\n",
		"scripts/run.sh": "#!/bin/sh\necho tidy\n",
	})
	rec := post(good)
	if rec.Code != http.StatusOK {
		t.Fatalf("inspect: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Skill struct {
			Name         string `json:"name"`
			Dependencies struct {
				Binaries []string `json:"binaries"`
			} `json:"dependencies"`
		} `json:"skill"`
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Skill.Name != "pdf-tidier" {
		t.Errorf("expected slugified name pdf-tidier, got %q", body.Skill.Name)
	}
	found := false
	for _, f := range body.Files {
		if f == "SKILL.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SKILL.md in tree preview: %v", body.Files)
	}

	bad := zipBytes(t, map[string]string{"README.md": "no skill here"})
	rec = post(bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("inspect without SKILL.md: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSkills_AgentTierInstallListRemove(t *testing.T) {
	r, st, ws, onClawDir := newSkillsTestEnv(t)

	prov := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "OpenAI"}
	if err := st.Providers().Create(nil, prov); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	agent := &domain.Agent{
		WorkspaceID: ws.ID,
		Slug:        "atlas",
		Name:        "Atlas",
		Role:        "ops",
		Brief:       "brief",
		ProviderID:  prov.ID,
		Model:       "gpt-4o",
	}
	if err := st.Agents().Create(nil, agent); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	rec := doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/skills", map[string]any{
		"name":        " deploy helper ",
		"description": "helps deploys",
		"body":        "Assist with deploys.",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent install: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	skillDir := filepath.Join(onClawDir, "workspaces", "acme", "agents", "atlas", "skills", "deploy-helper")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("expected agent-tier SKILL.md on disk: %v", err)
	}

	rec = doJSON(r, http.MethodGet, "/api/v1/workspaces/acme/agents/atlas/skills", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("agent list: expected 200, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"tier":"agent"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"deploy-helper"`)) {
		t.Errorf("expected agent-tier listing: %s", rec.Body.String())
	}

	// Duplicate name conflicts.
	rec = doJSON(r, http.MethodPost, "/api/v1/workspaces/acme/agents/atlas/skills", map[string]any{
		"name": "deploy-helper", "body": "again",
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate agent skill: expected 409, got %d", rec.Code)
	}

	rec = doJSON(r, http.MethodDelete, "/api/v1/workspaces/acme/agents/atlas/skills/deploy-helper", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("agent remove: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf("expected agent skill directory removed, stat err=%v", err)
	}
}
