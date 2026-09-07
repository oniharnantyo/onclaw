package handlers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oniharnantyo/onclaw/internal/agents/systemskills"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/skills"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Skill tiers as surfaced by the API (design D2): system entries are locked
// and always on, workspace entries are registry rows governed by the master
// switch, agent entries live in the owning agent's skills directory.
const (
	skillTierSystem    = "system"
	skillTierWorkspace = "workspace"
	skillTierAgent     = "agent"
)

// SkillDependenciesResponse is the declared dependency set stored on the
// registry row.
type SkillDependenciesResponse struct {
	Tools    []string `json:"tools,omitempty"`
	Binaries []string `json:"binaries,omitempty"`
	Python   []string `json:"python,omitempty"`
}

// SkillDependencyStatus is one resolved dependency entry of an install or
// re-check report.
type SkillDependencyStatus struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	InstallHint string `json:"install_hint,omitempty"`
}

// SkillResponse is one skill at any tier, shaped to the web client contract
// (web/src/lib/api.ts, ApiWorkspaceSkill).
type SkillResponse struct {
	ID               string                     `json:"id"`
	WorkspaceID      string                     `json:"workspace_id,omitempty"`
	Tier             string                     `json:"tier"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description"`
	Version          string                     `json:"version"`
	Source           string                     `json:"source"`
	Locked           bool                       `json:"locked,omitempty"`
	Enabled          bool                       `json:"enabled"`
	Dependencies     *SkillDependenciesResponse `json:"dependencies,omitempty"`
	DependencyStatus []SkillDependencyStatus    `json:"dependency_status,omitempty"`
	Body             string                     `json:"body,omitempty"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
}

// skillHandlers handles the workspace skill library endpoints: registry CRUD
// behind the install pipeline, inspection/discovery for the install wizard,
// and agent-tier skill install/remove under the agent endpoints.
type skillHandlers struct {
	install   *skills.InstallService
	registry  store.WorkspaceSkillStore
	agents    store.AgentStore
	onClawDir string
}

// NewSkillHandlers creates a new skillHandlers instance. onClawDir is the
// OnClaw root under which workspace and agent skill bodies live.
func NewSkillHandlers(install *skills.InstallService, registry store.WorkspaceSkillStore, agentStore store.AgentStore, onClawDir string) *skillHandlers {
	return &skillHandlers{
		install:   install,
		registry:  registry,
		agents:    agentStore,
		onClawDir: onClawDir,
	}
}

// ListSkills returns the workspace skill library: registry rows (workspace
// tier) plus the locked, always-on system tier (spec: "Listing includes the
// system tier").
func (h *skillHandlers) ListSkills(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	rows, err := h.registry.List(c.Request.Context(), ws.ID)
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]SkillResponse, 0, len(rows)+4)
	for i := range rows {
		row := rows[i]
		resp := skillResponseFromDomain(&row)
		resp.Dependencies = dependenciesResponse(row.Dependencies)
		items = append(items, resp)
	}

	system, err := systemskills.ListEmbedded()
	if err != nil {
		RespondError(c, err)
		return
	}
	for _, name := range system {
		items = append(items, systemSkillResponse(name))
	}

	RespondOK(c, gin.H{"skills": items})
}

// GetSkill returns one registry row including its SKILL.md body.
func (h *skillHandlers) GetSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	name := c.Param("name")

	row, err := h.registry.GetByName(c.Request.Context(), ws.ID, name)
	if err != nil {
		RespondError(c, err)
		return
	}

	resp := skillResponseFromDomain(row)
	resp.Dependencies = dependenciesResponse(row.Dependencies)
	resp.Body = h.readSkillBody(domain.WorkspaceSkillsDir(h.onClawDir, ws.Slug), name)
	RespondOK(c, gin.H{"skill": resp})
}

// CreateSkillRequest is the JSON create payload: a discriminated union on
// source (authored / git / fork). The upload source arrives as multipart.
type CreateSkillRequest struct {
	Source           string   `json:"source"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Body             string   `json:"body"`
	URL              string   `json:"url"`
	Ref              string   `json:"ref"`
	Token            string   `json:"token"`
	Names            []string `json:"names"`
	SystemSkill      string   `json:"system_skill"`
	EnableEverywhere bool     `json:"enable_everywhere"`
	ProvisionPython  bool     `json:"provision_python"`
	Overwrite        bool     `json:"overwrite"`
}

// CreateSkill installs skills from any of the four sources through the one
// install pipeline (design D4). Multipart bodies carry the upload source;
// JSON bodies carry authored/git/fork.
func (h *skillHandlers) CreateSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)

	in := skills.InstallInput{
		WorkspaceID: ws.ID,
		TenantSlug:  ws.Slug,
		OnClawDir:   h.onClawDir,
	}

	if strings.HasPrefix(c.ContentType(), "multipart/") {
		if err := h.bindUploadInput(c, &in); err != nil {
			RespondError(c, err)
			return
		}
	} else {
		var req CreateSkillRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondError(c, domain.ErrInvalid)
			return
		}
		in.Source = skills.Source(req.Source)
		in.Name, in.Description, in.Body = req.Name, req.Description, req.Body
		in.URL, in.Ref, in.Token, in.Selected = req.URL, req.Ref, req.Token, req.Names
		in.SystemSkill = req.SystemSkill
		in.EnableEverywhere, in.ProvisionPython, in.Overwrite = req.EnableEverywhere, req.ProvisionPython, req.Overwrite
	}

	results, err := h.install.Install(c.Request.Context(), in)
	if err != nil {
		RespondError(c, err)
		return
	}
	if len(results) == 0 {
		RespondError(c, fmt.Errorf("%w: no skills were installed", domain.ErrInvalid))
		return
	}

	RespondCreated(c, gin.H{
		"skill":             skillResponseFromPipeline(results[0].Skill),
		"dependency_status": dependencyStatusList(results[0].Report.Dependencies),
		"installed":         installList(results),
	})
}

// bindUploadInput reads the multipart upload variant: the zip archive under
// the `archive` field plus form-string install options.
func (h *skillHandlers) bindUploadInput(c *gin.Context, in *skills.InstallInput) error {
	file, err := c.FormFile("archive")
	if err != nil {
		return fmt.Errorf("%w: an archive file is required for upload installs", domain.ErrInvalid)
	}
	if file.Size > skills.MaxArchiveBytes {
		return fmt.Errorf("%w: archive exceeds the maximum size of %d bytes", domain.ErrPayloadTooLarge, skills.MaxArchiveBytes)
	}
	f, err := file.Open()
	if err != nil {
		return fmt.Errorf("%w: archive could not be read", domain.ErrInvalid)
	}
	defer f.Close()
	archive, err := io.ReadAll(io.LimitReader(f, skills.MaxArchiveBytes+1))
	if err != nil {
		return fmt.Errorf("%w: archive could not be read", domain.ErrInvalid)
	}
	if len(archive) > skills.MaxArchiveBytes {
		return fmt.Errorf("%w: archive exceeds the maximum size of %d bytes", domain.ErrPayloadTooLarge, skills.MaxArchiveBytes)
	}

	in.Source = skills.SourceUpload
	in.Archive = archive
	in.Name = c.PostForm("name")
	in.EnableEverywhere, err = parseFormBool(c, "enable_everywhere")
	if err != nil {
		return err
	}
	in.ProvisionPython, err = parseFormBool(c, "provision_python")
	if err != nil {
		return err
	}
	in.Overwrite, err = parseFormBool(c, "overwrite")
	return err
}

func parseFormBool(c *gin.Context, field string) (bool, error) {
	raw := c.PostForm(field)
	if raw == "" {
		return false, nil
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s must be a boolean", domain.ErrInvalid, field)
	}
	return val, nil
}

// UpdateSkillRequest holds the upsertable fields of a stored skill.
type UpdateSkillRequest struct {
	Description *string `json:"description,omitempty"`
	Body        *string `json:"body,omitempty"`
}

// UpdateSkill updates a skill's metadata and/or SKILL.md body.
func (h *skillHandlers) UpdateSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	name := c.Param("name")

	var req UpdateSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}
	if req.Description == nil && req.Body == nil {
		RespondError(c, fmt.Errorf("%w: no fields to update", domain.ErrInvalid))
		return
	}

	row, err := h.registry.GetByName(c.Request.Context(), ws.ID, name)
	if err != nil {
		RespondError(c, err)
		return
	}

	if req.Description != nil {
		row.Description = strings.TrimSpace(*req.Description)
	}
	if req.Body != nil {
		if strings.TrimSpace(*req.Body) == "" {
			RespondError(c, fmt.Errorf("%w: skill body cannot be empty", domain.ErrInvalid))
			return
		}
		skillDir := filepath.Join(domain.WorkspaceSkillsDir(h.onClawDir, ws.Slug), row.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			RespondError(c, fmt.Errorf("failed to write skill body: %w", err))
			return
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(*req.Body), 0o644); err != nil {
			RespondError(c, fmt.Errorf("failed to write skill body: %w", err))
			return
		}
	}

	if req.Description != nil {
		if err := h.registry.Update(c.Request.Context(), row); err != nil {
			RespondError(c, err)
			return
		}
	}

	refreshed, err := h.registry.GetByName(c.Request.Context(), ws.ID, name)
	if err != nil {
		RespondError(c, err)
		return
	}
	resp := skillResponseFromDomain(refreshed)
	resp.Dependencies = dependenciesResponse(refreshed.Dependencies)
	resp.Body = h.readSkillBody(domain.WorkspaceSkillsDir(h.onClawDir, ws.Slug), name)
	RespondOK(c, gin.H{"skill": resp})
}

// PatchSkillRequest carries the master enable switch.
type PatchSkillRequest struct {
	Enabled *bool `json:"enabled"`
}

// PatchSkill flips the workspace master switch: enabled skills attach to
// every agent, disabled skills to none (design D2).
func (h *skillHandlers) PatchSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	name := c.Param("name")

	var req PatchSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		RespondError(c, fmt.Errorf("%w: enabled (bool) is required", domain.ErrInvalid))
		return
	}

	row, err := h.registry.GetByName(c.Request.Context(), ws.ID, name)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.registry.SetEnabled(c.Request.Context(), ws.ID, row.ID, *req.Enabled); err != nil {
		RespondError(c, err)
		return
	}

	refreshed, err := h.registry.GetByName(c.Request.Context(), ws.ID, name)
	if err != nil {
		RespondError(c, err)
		return
	}
	resp := skillResponseFromDomain(refreshed)
	resp.Dependencies = dependenciesResponse(refreshed.Dependencies)
	RespondOK(c, gin.H{"skill": resp})
}

// DeleteSkill uninstalls a workspace skill: registry row and body tree.
func (h *skillHandlers) DeleteSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	name := c.Param("name")

	if err := h.install.Uninstall(c.Request.Context(), ws.ID, ws.Slug, h.onClawDir, name); err != nil {
		RespondError(c, err)
		return
	}
	RespondNoContent(c)
}

// RecheckSkill re-probes every dependency kind and persists the refreshed
// statuses on the row.
func (h *skillHandlers) RecheckSkill(c *gin.Context) {
	ws := MustCurrentWorkspace(c)
	name := c.Param("name")

	row, err := h.install.RecheckDependencies(c.Request.Context(), ws.ID, ws.Slug, h.onClawDir, name)
	if err != nil {
		RespondError(c, err)
		return
	}
	resp := skillResponseFromPipeline(row)
	resp.DependencyStatus = dependencyStatusList(row.Dependencies)
	RespondOK(c, gin.H{"skill": resp})
}

// InspectUpload validates an uploaded archive without installing anything:
// tree preview plus the inferred dependency report (wizard content step).
func (h *skillHandlers) InspectUpload(c *gin.Context) {

	file, err := c.FormFile("archive")
	if err != nil {
		RespondError(c, fmt.Errorf("%w: an archive file is required", domain.ErrInvalid))
		return
	}
	if file.Size > skills.MaxArchiveBytes {
		RespondError(c, fmt.Errorf("%w: archive exceeds the maximum size of %d bytes", domain.ErrPayloadTooLarge, skills.MaxArchiveBytes))
		return
	}
	f, err := file.Open()
	if err != nil {
		RespondError(c, fmt.Errorf("%w: archive could not be read", domain.ErrInvalid))
		return
	}
	defer f.Close()
	archive, err := io.ReadAll(io.LimitReader(f, skills.MaxArchiveBytes+1))
	if err != nil {
		RespondError(c, fmt.Errorf("%w: archive could not be read", domain.ErrInvalid))
		return
	}
	if len(archive) > skills.MaxArchiveBytes {
		RespondError(c, fmt.Errorf("%w: archive exceeds the maximum size of %d bytes", domain.ErrPayloadTooLarge, skills.MaxArchiveBytes))
		return
	}

	staged, err := skills.StageZip(archive)
	if err != nil {
		RespondError(c, err)
		return
	}

	files := make([]string, 0, len(staged.Files))
	for _, file := range staged.Files {
		files = append(files, file.Path)
	}
	deps := skills.InferDependencies(staged)
	depsResponse := SkillDependenciesResponse{
		Tools:    make([]string, 0, len(deps.Tools)),
		Binaries: make([]string, 0, len(deps.Binaries)),
		Python:   make([]string, 0, len(deps.Python)),
	}
	for _, dep := range deps.Tools {
		depsResponse.Tools = append(depsResponse.Tools, dep.Name)
	}
	for _, dep := range deps.Binaries {
		depsResponse.Binaries = append(depsResponse.Binaries, dep.Name)
	}
	for _, dep := range deps.Python {
		depsResponse.Python = append(depsResponse.Python, dep.Requirement)
	}

	RespondOK(c, gin.H{
		"skill": gin.H{
			"name":         staged.Name,
			"description":  staged.Description,
			"dependencies": depsResponse,
		},
		"files": files,
	})
}

// InspectGitRequest asks the service to fetch a git/URL target for skill
// discovery (wizard multi-select step).
type InspectGitRequest struct {
	URL   string `json:"url"`
	Ref   string `json:"ref"`
	Token string `json:"token"`
}

// InspectGit fetches a git/URL target and lists its SKILL.md-bearing
// directories. The token is one-time and never persisted.
func (h *skillHandlers) InspectGit(c *gin.Context) {
	var req InspectGitRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		RespondError(c, fmt.Errorf("%w: url is required", domain.ErrInvalid))
		return
	}

	discovered, err := h.install.Discover(c.Request.Context(), skills.DiscoverInput{URL: req.URL, Ref: req.Ref, Token: req.Token})
	if err != nil {
		RespondError(c, err)
		return
	}

	items := make([]gin.H, 0, len(discovered))
	for _, d := range discovered {
		items = append(items, gin.H{"name": d.Name, "description": d.Description})
	}
	RespondOK(c, gin.H{"skills": items})
}

// resolveSkillAgent resolves an agent in the workspace by slug first, then ID.
func (h *skillHandlers) resolveSkillAgent(c *gin.Context) (*domain.Workspace, *domain.Agent, error) {
	ws := MustCurrentWorkspace(c)
	param := c.Param("agent")
	if param == "" {
		return ws, nil, domain.ErrNotFound
	}
	agent, err := h.agents.BySlug(c.Request.Context(), ws.ID, param)
	if err == nil {
		return ws, agent, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return ws, nil, err
	}
	agent, err = h.agents.ByID(c.Request.Context(), ws.ID, param)
	return ws, agent, err
}

// ListAgentSkills returns the agent-tier skills: bodies in the agent's own
// skills directory; presence is the state.
func (h *skillHandlers) ListAgentSkills(c *gin.Context) {
	ws, agent, err := h.resolveSkillAgent(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	dir := domain.AgentSkillsDir(h.onClawDir, ws.Slug, agent.Slug)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		RespondError(c, fmt.Errorf("failed to read agent skills directory: %w", err))
		return
	}

	items := make([]SkillResponse, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		body := h.readSkillBody(dir, entry.Name())
		if body == "" {
			continue // not a skill directory
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		items = append(items, SkillResponse{
			ID:          "agent/" + agent.Slug + "/" + entry.Name(),
			Tier:        skillTierAgent,
			Name:        entry.Name(),
			Description: skills.FrontmatterField(body, "description"),
			Version:     frontmatterVersionOrDefault(body),
			Source:      string(skills.SourceAuthored),
			Enabled:     true,
			Body:        body,
			CreatedAt:   info.ModTime(),
			UpdatedAt:   info.ModTime(),
		})
	}
	RespondOK(c, gin.H{"skills": items})
}

// InstallAgentSkillRequest authors a skill into the owning agent's skills
// directory (agent tier).
type InstallAgentSkillRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// InstallAgentSkill writes an authored skill into the agent-tier directory.
func (h *skillHandlers) InstallAgentSkill(c *gin.Context) {
	ws, agent, err := h.resolveSkillAgent(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	var req InstallAgentSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, domain.ErrInvalid)
		return
	}

	staged := skills.StageAuthor(req.Name, req.Description, req.Body)
	if staged.Name == "" {
		RespondError(c, fmt.Errorf("%w: a skill name is required", domain.ErrInvalid))
		return
	}
	if err := domain.ValidateSlug(staged.Name); err != nil {
		RespondError(c, err)
		return
	}

	skillDir := filepath.Join(domain.AgentSkillsDir(h.onClawDir, ws.Slug, agent.Slug), staged.Name)
	if _, err := os.Stat(skillDir); err == nil {
		RespondError(c, fmt.Errorf("%w: agent skill %q already exists", domain.ErrConflict, staged.Name))
		return
	}
	for _, file := range staged.Files {
		target := filepath.Join(skillDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			RespondError(c, fmt.Errorf("failed to write agent skill: %w", err))
			return
		}
		mode := os.FileMode(file.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(target, file.Content, mode); err != nil {
			RespondError(c, fmt.Errorf("failed to write agent skill: %w", err))
			return
		}
	}

	RespondCreated(c, gin.H{"skill": SkillResponse{
		ID:          "agent/" + agent.Slug + "/" + staged.Name,
		WorkspaceID: ws.ID,
		Tier:        skillTierAgent,
		Name:        staged.Name,
		Description: staged.Description,
		Version:     staged.Version,
		Source:      string(staged.Source),
		Enabled:     true,
		Body:        req.Body,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}})
}

// RemoveAgentSkill deletes an agent-tier skill directory.
func (h *skillHandlers) RemoveAgentSkill(c *gin.Context) {
	ws, agent, err := h.resolveSkillAgent(c)
	if err != nil {
		RespondError(c, err)
		return
	}

	name := c.Param("name")
	if err := domain.ValidateSlug(name); err != nil {
		RespondError(c, err)
		return
	}

	skillDir := filepath.Join(domain.AgentSkillsDir(h.onClawDir, ws.Slug, agent.Slug), name)
	if _, err := os.Stat(skillDir); err != nil {
		RespondError(c, domain.ErrNotFound)
		return
	}
	if err := os.RemoveAll(skillDir); err != nil {
		RespondError(c, fmt.Errorf("failed to remove agent skill: %w", err))
		return
	}
	RespondNoContent(c)
}

// readSkillBody returns the SKILL.md content of a skill directory, or "" when
// the directory or file does not exist.
func (h *skillHandlers) readSkillBody(skillsRoot, name string) string {
	body, err := os.ReadFile(filepath.Join(skillsRoot, name, "SKILL.md"))
	if err != nil {
		return ""
	}
	return string(body)
}

func frontmatterVersionOrDefault(content string) string {
	if version := skills.FrontmatterField(content, "version"); version != "" {
		return version
	}
	return skills.AuthorVersion
}

// systemSkillResponse renders one locked, always-on embedded system skill.
func systemSkillResponse(name string) SkillResponse {
	description, version := "", skills.AuthorVersion
	if content, err := systemskills.GetEmbeddedSkill(name); err == nil {
		description = skills.FrontmatterField(content, "description")
		version = frontmatterVersionOrDefault(content)
	}
	return SkillResponse{
		ID:          "system/" + name,
		Tier:        skillTierSystem,
		Name:        name,
		Description: description,
		Version:     version,
		Source:      skillTierSystem,
		Locked:      true,
		Enabled:     true,
	}
}

func skillResponseFromDomain(row *domain.WorkspaceSkill) SkillResponse {
	return SkillResponse{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Tier:        skillTierWorkspace,
		Name:        row.Name,
		Description: row.Description,
		Version:     row.Version,
		Source:      string(row.Source),
		Enabled:     row.Enabled,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func skillResponseFromPipeline(row *skills.Skill) SkillResponse {
	deps := SkillDependenciesResponse{
		Tools:    make([]string, 0, len(row.Dependencies.Tools)),
		Binaries: make([]string, 0, len(row.Dependencies.Binaries)),
		Python:   make([]string, 0, len(row.Dependencies.Python)),
	}
	for _, dep := range row.Dependencies.Tools {
		deps.Tools = append(deps.Tools, dep.Name)
	}
	for _, dep := range row.Dependencies.Binaries {
		deps.Binaries = append(deps.Binaries, dep.Name)
	}
	for _, dep := range row.Dependencies.Python {
		deps.Python = append(deps.Python, dep.Requirement)
	}
	return SkillResponse{
		WorkspaceID:      row.WorkspaceID,
		Tier:             skillTierWorkspace,
		Name:             row.Name,
		Description:      row.Description,
		Version:          row.Version,
		Source:           string(row.Source),
		Enabled:          row.Enabled,
		Dependencies:     &deps,
		DependencyStatus: dependencyStatusList(row.Dependencies),
		CreatedAt:        row.InstalledAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func installList(results []*skills.InstallResult) []SkillResponse {
	items := make([]SkillResponse, 0, len(results))
	for _, result := range results {
		items = append(items, skillResponseFromPipeline(result.Skill))
	}
	return items
}

func dependenciesResponse(deps domain.SkillDependencies) *SkillDependenciesResponse {
	if len(deps.Tools) == 0 && len(deps.Binaries) == 0 && len(deps.Python) == 0 {
		return nil
	}
	return &SkillDependenciesResponse{Tools: deps.Tools, Binaries: deps.Binaries, Python: deps.Python}
}

func dependencyStatusList(set skills.DependencySet) []SkillDependencyStatus {
	statuses := make([]SkillDependencyStatus, 0, len(set.Tools)+len(set.Binaries)+len(set.Python))
	for _, dep := range set.Tools {
		statuses = append(statuses, SkillDependencyStatus{Kind: "tools", Name: dep.Name, Status: string(dep.Status)})
	}
	for _, dep := range set.Binaries {
		statuses = append(statuses, SkillDependencyStatus{Kind: "binaries", Name: dep.Name, Status: string(dep.Status), InstallHint: dep.InstallHint})
	}
	for _, dep := range set.Python {
		statuses = append(statuses, SkillDependencyStatus{Kind: "python", Name: dep.Requirement, Status: string(dep.Status)})
	}
	return statuses
}
