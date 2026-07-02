package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/api/httpx"
	"github.com/oniharnantyo/onclaw/internal/api/service"
)

// ListSkills handles GET /api/skills.
func (h *Handler) ListSkills(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp, err := h.svc.ListSkills(ctx)
	if err != nil {
		h.handleError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// DiscoverSkills handles POST /api/skills/discover.
func (h *Handler) DiscoverSkills(w http.ResponseWriter, r *http.Request) {
	var input service.DiscoverInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Source == "" {
		httpx.Error(w, http.StatusBadRequest, "Source is required")
		return
	}

	resp, err := h.svc.DiscoverSkills(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// InstallSkills handles POST /api/skills.
func (h *Handler) InstallSkills(w http.ResponseWriter, r *http.Request) {
	var input service.InstallSkillInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.Source == "" {
		httpx.Error(w, http.StatusBadRequest, "Source is required")
		return
	}

	resp, err := h.svc.InstallSkills(r.Context(), input)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, resp)
}

// GetSkill handles GET /api/skills/{name}.
func (h *Handler) GetSkill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "global"
	}

	resp, err := h.svc.GetSkill(ctx, name, scope)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// DeleteSkill handles DELETE /api/skills/{name}.
func (h *Handler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "global"
	}

	if err := h.svc.RemoveSkill(ctx, name, scope); err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// UpdateSkill handles POST /api/skills/{name}/update.
func (h *Handler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PathValue("name")
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "global"
	}

	resp, err := h.svc.UpdateSkill(ctx, name, scope)
	if err != nil {
		h.handleError(w, err)
		return
	}

	httpx.JSON(w, http.StatusOK, resp)
}

// UploadSkill handles POST /api/skills/upload
func (h *Handler) UploadSkill(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (limit size to 50MB)
	err := r.ParseMultipartForm(50 << 20)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Missing file field")
		return
	}
	defer file.Close()

	// Get file extension
	origName := header.Filename
	ext := filepath.Ext(origName)
	if ext != ".zip" && ext != ".tar.gz" && ext != ".tgz" {
		if strings.HasSuffix(origName, ".tar.gz") {
			ext = ".tar.gz"
		} else {
			httpx.Error(w, http.StatusBadRequest, "Unsupported archive type (must be .zip, .tar.gz, or .tgz)")
			return
		}
	}

	// Create a temp directory
	uploadDir, err := os.MkdirTemp("", "onclaw-upload-*")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Failed to create temp directory: "+err.Error())
		return
	}

	tempFilePath := filepath.Join(uploadDir, origName)
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		os.RemoveAll(uploadDir)
		httpx.Error(w, http.StatusInternalServerError, "Failed to create temp file: "+err.Error())
		return
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		os.RemoveAll(uploadDir)
		httpx.Error(w, http.StatusInternalServerError, "Failed to save uploaded file: "+err.Error())
		return
	}

	// Discover skills from the uploaded local archive
	resp, err := h.svc.DiscoverSkills(r.Context(), service.DiscoverInput{
		Source: tempFilePath,
	})
	if err != nil {
		os.RemoveAll(uploadDir)
		h.handleError(w, err)
		return
	}

	type uploadResult struct {
		Source      string                    `json:"source"`
		PackageName string                    `json:"package_name"`
		IsPlugin    bool                      `json:"is_plugin"`
		Skills      []service.DiscoveredSkill `json:"skills"`
	}

	httpx.JSON(w, http.StatusOK, uploadResult{
		Source:      tempFilePath,
		PackageName: resp.PackageName,
		IsPlugin:    resp.IsPlugin,
		Skills:      resp.Skills,
	})
}
