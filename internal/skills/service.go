package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/agents/systemskills"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// AuthorVersion is the version stamped on authored skills (spec:
// "Author install creates a registry row").
const AuthorVersion = "0.1.0"

// TxStores carries transaction-scoped sub-stores: the composition root
// binds this to store.WithTx so the install service can perform the row
// write, the tool-gate write, and the bulk agent-allowlist update as one
// transaction — the documented transaction-seam exception (AGENTS.md,
// design D8).
type TxStores struct {
	Skills       Store
	Agents       store.AgentStore
	ToolSettings store.ToolSettingsStore
}

// TxProvider opens a transaction and hands the callback transaction-scoped
// sub-stores. The zero behavior (nil) runs the callback against the
// service's own stores without a transaction.
type TxProvider func(ctx context.Context, fn func(ctx context.Context, tx TxStores) error) error

// InstallInput is one install request covering all four sources; exactly
// the fields of the chosen Source are read.
type InstallInput struct {
	WorkspaceID string
	TenantSlug  string
	OnClawDir   string
	Source      Source

	// Author.
	Name        string
	Description string
	Body        string

	// Upload: exactly one of Archive (zip bytes) or FolderDir (a
	// server-side walked folder) must be set.
	Archive   []byte
	FolderDir string

	// Git/URL: URL is a git remote (cloned shallow) or, when it ends in
	// .zip, an archive download. Token is one-time and never persisted.
	// Selected filters discovered skills by Name or RelDir; empty selects
	// all discovered.
	URL      string
	Ref      string
	Token    string
	Selected []string

	// Fork: SystemSkill names the embedded system skill to copy.
	SystemSkill string

	// Behavior.
	Overwrite       bool
	EnableEverywhere bool
	ProvisionPython  bool
}

// DiscoverInput asks the service to fetch a git/URL target and list the
// SKILL.md-bearing directories it contains (wizard multi-select step).
type DiscoverInput struct {
	URL   string
	Ref   string
	Token string
}

// InstallResult reports one installed skill and its resolved dependency
// state.
type InstallResult struct {
	Skill  *Skill
	Report DependencyReport
}

// DependencyReport is the install-time dependency report surfaced by the
// wizard: statuses per kind plus human-facing guidance.
type DependencyReport struct {
	Dependencies DependencySet
	Warnings     []string
}

// InstallService runs the one install pipeline behind the four sources:
// validate → materialize → write files → insert row → dependency step
// (design D4). Files land before the row so a crashed install leaves at
// most an orphan directory, never a phantom row; a failed row insert
// removes the written tree.
type InstallService struct {
	skills       Store
	agents       store.AgentStore
	toolSettings store.ToolSettingsStore
	tx           TxProvider
	fetcher      *Fetcher
	runner       CommandRunner
	now          func() time.Time
}

// InstallOption tunes behavioral wiring (knobs like archive caps are
// constants by design; options carry collaborators).
type InstallOption func(*InstallService)

// WithTxProvider binds the transaction seam.
func WithTxProvider(p TxProvider) InstallOption {
	return func(s *InstallService) { s.tx = p }
}

// WithFetcher overrides the default URL/git fetcher.
func WithFetcher(f *Fetcher) InstallOption {
	return func(s *InstallService) { s.fetcher = f }
}

// WithCommandRunner overrides the exec seam (tests).
func WithCommandRunner(r CommandRunner) InstallOption {
	return func(s *InstallService) { s.runner = r }
}

// WithNow overrides the clock (tests).
func WithNow(fn func() time.Time) InstallOption {
	return func(s *InstallService) { s.now = fn }
}

// NewInstallService builds the service from its granular dependencies. The
// store trio exists because "enable everywhere" writes the workspace tool
// gate and every agent's allowlist alongside the registry row in one
// transaction (the documented exception to the one-sub-interface rule).
func NewInstallService(skills Store, agents store.AgentStore, toolSettings store.ToolSettingsStore, opts ...InstallOption) *InstallService {
	svc := &InstallService{
		skills:       skills,
		agents:       agents,
		toolSettings: toolSettings,
		runner:       NewExecRunner(),
		now:          time.Now,
	}
	for _, opt := range opts {
		opt(svc)
	}
	if svc.fetcher == nil {
		svc.fetcher = NewFetcher(svc.runner, nil)
	}
	return svc
}

// Discover fetches a git/URL target into a scratch tree and returns every
// directory containing a SKILL.md. `.zip` URLs are downloaded; anything
// else is shallow-cloned.
func (s *InstallService) Discover(ctx context.Context, in DiscoverInput) ([]DiscoveredSkill, error) {
	if strings.HasSuffix(strings.ToLower(in.URL), ".zip") {
		body, err := s.fetcher.FetchArchive(ctx, in.URL, in.Token)
		if err != nil {
			return nil, err
		}
		staged, err := StageZip(body)
		if err != nil {
			return nil, err
		}
		return DiscoverStaged(staged), nil
	}
	tmp, err := os.MkdirTemp("", "onclaw-skill-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := s.fetcher.CloneGit(ctx, tmp, in.URL, in.Ref, in.Token); err != nil {
		return nil, err
	}
	return DiscoverSkills(tmp)
}

// DiscoverStaged maps a staged single-archive skill to a one-entry
// discovery list.
func DiscoverStaged(staged *StagedSkill) []DiscoveredSkill {
	return []DiscoveredSkill{{
		Name:        staged.Name,
		RelDir:      "",
		Description: staged.Description,
	}}
}

// Install runs the full pipeline. Git sources may install several skills
// (one per selected directory); every other source installs exactly one.
func (s *InstallService) Install(ctx context.Context, in InstallInput) ([]*InstallResult, error) {
	if err := validateInput(&in); err != nil {
		return nil, err
	}
	staged, err := s.materialize(ctx, in)
	if err != nil {
		return nil, err
	}
	results := make([]*InstallResult, 0, len(staged))
	for _, sk := range staged {
		result, err := s.installOne(ctx, in, sk)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// Uninstall removes a workspace skill: registry row and body tree. The
// shared venv is left alone — it serves every enabled skill.
func (s *InstallService) Uninstall(ctx context.Context, workspaceID, tenantSlug, onClawDir, name string) error {
	if _, err := s.skills.Get(ctx, workspaceID, name); err != nil {
		return err
	}
	if err := s.skills.Delete(ctx, workspaceID, name); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(domain.WorkspaceSkillsDir(onClawDir, tenantSlug), name))
}

// Enable flips the workspace master switch.
func (s *InstallService) Enable(ctx context.Context, workspaceID, name string, enabled bool) error {
	return s.skills.SetEnabled(ctx, workspaceID, name, enabled)
}

// RecheckDependencies re-probes every dependency kind for a stored row —
// binaries via LookPath, tools via gate+allowlists, python via the
// provisioner's Check — and persists the refreshed statuses (spec:
// "Dependency status SHALL be re-checkable on demand").
func (s *InstallService) RecheckDependencies(ctx context.Context, workspaceID, tenantSlug, onClawDir, name string) (*Skill, error) {
	row, err := s.skills.Get(ctx, workspaceID, name)
	if err != nil {
		return nil, err
	}
	set, _, err := s.resolveDependencies(ctx, workspaceID, domain.WorkspaceSkillsDir(onClawDir, tenantSlug), row.Dependencies, false, false)
	if err != nil {
		return nil, err
	}
	row.Dependencies = set
	row.UpdatedAt = s.now()
	if err := s.skills.Update(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// EnableEverywhere adds each tool to the workspace tool gate and to every
// agent's tool allowlist in one service call — the transaction seam
// (spec: "Enable-everywhere satisfies tool dependencies").
func (s *InstallService) EnableEverywhere(ctx context.Context, workspaceID string, toolNames []string) error {
	run := func(ctx context.Context, tx TxStores) error {
		for _, tool := range toolNames {
			if err := enableToolInGate(ctx, tx.ToolSettings, workspaceID, tool); err != nil {
				return err
			}
		}
		agents, err := tx.Agents.ListForWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		for i := range agents {
			if hasAllTools(agents[i].Tools, toolNames) {
				continue
			}
			agents[i].Tools = mergeTools(agents[i].Tools, toolNames)
			if err := tx.Agents.Update(ctx, &agents[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if s.tx != nil {
		return s.tx(ctx, run)
	}
	return run(ctx, TxStores{Skills: s.skills, Agents: s.agents, ToolSettings: s.toolSettings})
}

// validateInput checks the common fields and source-specific payloads.
func validateInput(in *InstallInput) error {
	if in.WorkspaceID == "" || in.TenantSlug == "" || in.OnClawDir == "" {
		return fmt.Errorf("%w: workspace id, tenant slug, and onClaw dir are required", domain.ErrInvalid)
	}
	if !ValidSource(in.Source) {
		return fmt.Errorf("%w: unknown install source %q", domain.ErrInvalid, in.Source)
	}
	switch in.Source {
	case SourceAuthored:
		if strings.TrimSpace(in.Name) == "" {
			return fmt.Errorf("%w: skill name is required for authored skills", domain.ErrInvalid)
		}
		if strings.TrimSpace(in.Body) == "" {
			return fmt.Errorf("%w: skill body is required for authored skills", domain.ErrInvalid)
		}
	case SourceUpload:
		if len(in.Archive) == 0 && in.FolderDir == "" {
			return fmt.Errorf("%w: an archive or folder is required for uploads", domain.ErrInvalid)
		}
	case SourceGit:
		if in.URL == "" {
			return fmt.Errorf("%w: a URL is required for git/url installs", domain.ErrInvalid)
		}
	case SourceFork:
		if in.SystemSkill == "" {
			return fmt.Errorf("%w: a system skill name is required for forks", domain.ErrInvalid)
		}
	}
	return nil
}

// materialize turns an input into staged skills (validate + unpack/clone
// steps of the pipeline).
func (s *InstallService) materialize(ctx context.Context, in InstallInput) ([]*StagedSkill, error) {
	switch in.Source {
	case SourceAuthored:
		return []*StagedSkill{StageAuthor(in.Name, in.Description, in.Body)}, nil
	case SourceUpload:
		if len(in.Archive) > 0 {
			staged, err := StageZip(in.Archive)
			if err != nil {
				return nil, err
			}
			if in.Name != "" {
				staged.Name = SlugifyName(in.Name)
			}
			return []*StagedSkill{staged}, nil
		}
		staged, err := StageDirectory(in.FolderDir)
		if err != nil {
			return nil, err
		}
		if in.Name != "" {
			staged.Name = SlugifyName(in.Name)
		}
		return []*StagedSkill{staged}, nil
	case SourceFork:
		staged, err := StageFork(in.SystemSkill)
		if err != nil {
			return nil, err
		}
		if in.Name != "" {
			staged.Name = SlugifyName(in.Name)
		}
		return []*StagedSkill{staged}, nil
	case SourceGit:
		return s.materializeGit(ctx, in)
	}
	return nil, fmt.Errorf("%w: unknown install source %q", domain.ErrInvalid, in.Source)
}

// materializeGit fetches the target and stages every selected skill
// directory.
func (s *InstallService) materializeGit(ctx context.Context, in InstallInput) ([]*StagedSkill, error) {
	discovered, err := s.Discover(ctx, DiscoverInput{URL: in.URL, Ref: in.Ref, Token: in.Token})
	if err != nil {
		return nil, err
	}
	if len(discovered) == 0 {
		return nil, fmt.Errorf("%w: no skill directories (containing SKILL.md) found in %q", domain.ErrInvalid, in.URL)
	}
	selected := discovered
	if len(in.Selected) > 0 {
		want := map[string]bool{}
		for _, name := range in.Selected {
			want[name] = true
		}
		selected = selected[:0]
		for _, d := range discovered {
			if want[d.Name] || want[d.RelDir] {
				selected = append(selected, d)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("%w: none of the selected skills exist in %q", domain.ErrInvalid, in.URL)
		}
	}

	if strings.HasSuffix(strings.ToLower(in.URL), ".zip") {
		body, err := s.fetcher.FetchArchive(ctx, in.URL, in.Token)
		if err != nil {
			return nil, err
		}
		staged, err := StageZip(body)
		if err != nil {
			return nil, err
		}
		staged.Source = SourceGit
		return []*StagedSkill{staged}, nil
	}

	tmp, err := os.MkdirTemp("", "onclaw-skill-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := s.fetcher.CloneGit(ctx, tmp, in.URL, in.Ref, in.Token); err != nil {
		return nil, err
	}
	stagedSkills := make([]*StagedSkill, 0, len(selected))
	for _, d := range selected {
		staged, err := StageDirectory(filepath.Join(tmp, filepath.FromSlash(d.RelDir)))
		if err != nil {
			return nil, err
		}
		staged.Name = d.Name
		staged.Source = SourceGit
		stagedSkills = append(stagedSkills, staged)
	}
	return stagedSkills, nil
}

// StageAuthor synthesizes a SKILL.md from name/description/body — the
// author source (spec: version 0.1.0).
func StageAuthor(name, description, body string) *StagedSkill {
	name = SlugifyName(name)
	var frontmatter strings.Builder
	frontmatter.WriteString("---\n")
	fmt.Fprintf(&frontmatter, "name: %s\n", name)
	if description != "" {
		fmt.Fprintf(&frontmatter, "description: %s\n", strings.ReplaceAll(description, "\n", " "))
	}
	fmt.Fprintf(&frontmatter, "version: %s\n", AuthorVersion)
	frontmatter.WriteString("---\n")
	return &StagedSkill{
		Name:        name,
		Description: description,
		Version:     AuthorVersion,
		Source:      SourceAuthored,
		Files: []StagedFile{{
			Path:    "SKILL.md",
			Content: []byte(frontmatter.String() + "\n" + body + "\n"),
			Mode:    0o644,
		}},
	}
}

// StageFork copies an embedded system skill into the workspace tier — the
// only customization path for system skills (spec: fork source).
func StageFork(systemSkillName string) (*StagedSkill, error) {
	content, err := systemskills.GetEmbeddedSkill(systemSkillName)
	if err != nil {
		return nil, fmt.Errorf("%w: unknown system skill %q", domain.ErrInvalid, systemSkillName)
	}
	return &StagedSkill{
		Name:        SlugifyName(systemSkillName),
		Description: FrontmatterField(content, "description"),
		Version:     AuthorVersion,
		Source:      SourceFork,
		Files: []StagedFile{{
			Path:    "SKILL.md",
			Content: []byte(content),
			Mode:    0o644,
		}},
	}, nil
}

// installOne executes the pipeline for one staged skill.
func (s *InstallService) installOne(ctx context.Context, in InstallInput, staged *StagedSkill) (*InstallResult, error) {
	if staged.Name == "" {
		return nil, fmt.Errorf("%w: skill name could not be derived (set an explicit name)", domain.ErrInvalid)
	}
	if err := domain.ValidateSlug(staged.Name); err != nil {
		return nil, err
	}
	if err := validateStagedFiles(staged.Files); err != nil {
		return nil, err
	}

	skillsRoot := domain.WorkspaceSkillsDir(in.OnClawDir, in.TenantSlug)
	skillDir := filepath.Join(skillsRoot, staged.Name)

	existing, err := s.skills.Get(ctx, in.WorkspaceID, staged.Name)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if existing != nil && !in.Overwrite {
		return nil, fmt.Errorf("%w: workspace skill %q already exists (confirm overwrite to replace it)", domain.ErrConflict, staged.Name)
	}

	// Dependency inference happens on the staged bytes: imported files stay
	// byte-faithful; the resolved set lands on the row (design D5).
	deps := InferDependencies(staged)

	// Write files before the row: a crash leaves at most an orphan tree,
	// never a phantom row (design D4). Staged into a dot-dir, then swapped.
	if err := writeStaged(skillsRoot, staged); err != nil {
		return nil, err
	}

	now := s.now()
	row := &Skill{
		WorkspaceID:  in.WorkspaceID,
		Name:         staged.Name,
		Description:  staged.Description,
		Version:      staged.Version,
		Source:       staged.Source,
		Enabled:      true,
		Dependencies: deps,
		InstalledAt:  now,
		UpdatedAt:    now,
	}
	if staged.Version == "" {
		row.Version = AuthorVersion
	}
	if existing != nil {
		// Overwrite-with-confirm: tree replaced, version bumped, row
		// updated; enabled state survives (spec: registry name uniqueness).
		row.Enabled = existing.Enabled
		row.InstalledAt = existing.InstalledAt
		row.Version = bumpPatch(existing.Version)
		if err := s.skills.Update(ctx, row); err != nil {
			os.RemoveAll(skillDir)
			return nil, err
		}
	} else if err := s.skills.Create(ctx, row); err != nil {
		// Row-insert failure removes the written tree (orphan cleanup).
		os.RemoveAll(skillDir)
		return nil, err
	}

	// Dependency step: resolve statuses, optionally provisioning and
	// enabling tools everywhere, then persist the resolved set.
	set, warnings, err := s.resolveDependencies(ctx, in.WorkspaceID, skillsRoot, deps, in.EnableEverywhere, in.ProvisionPython)
	if err != nil {
		return nil, err
	}
	row.Dependencies = set
	row.UpdatedAt = s.now()
	if err := s.skills.Update(ctx, row); err != nil {
		return nil, err
	}
	return &InstallResult{Skill: row, Report: DependencyReport{Dependencies: set, Warnings: warnings}}, nil
}

// resolveDependencies computes statuses for all three kinds. Tool misses
// with enableEverywhere become gate + allowlist writes in one call; python
// with provision runs the provisioner over the union of enabled skills'
// requirements.
func (s *InstallService) resolveDependencies(ctx context.Context, workspaceID, skillsRoot string, set DependencySet, enableEverywhere, provision bool) (DependencySet, []string, error) {
	var warnings []string
	resolved := set

	if len(set.Tools) > 0 {
		names := make([]string, 0, len(set.Tools))
		for _, t := range set.Tools {
			names = append(names, t.Name)
		}
		toolStatuses, err := s.toolStatuses(ctx, workspaceID, names)
		if err != nil {
			return resolved, warnings, err
		}
		if enableEverywhere {
			var missing []string
			for _, t := range toolStatuses {
				if t.Status != DepMet {
					missing = append(missing, t.Name)
				}
			}
			if len(missing) > 0 {
				if err := s.EnableEverywhere(ctx, workspaceID, missing); err != nil {
					return resolved, warnings, err
				}
				toolStatuses, err = s.toolStatuses(ctx, workspaceID, names)
				if err != nil {
					return resolved, warnings, err
				}
			}
		}
		resolved.Tools = toolStatuses
	}

	if len(set.Binaries) > 0 {
		names := make([]string, 0, len(set.Binaries))
		for _, b := range set.Binaries {
			names = append(names, b.Name)
		}
		resolved.Binaries = CheckBinaries(s.runner, names)
		for _, b := range resolved.Binaries {
			if b.Status == DepMissing {
				warnings = append(warnings, fmt.Sprintf("binary %q is missing from the server PATH — install it with %q; the skill installs with a warning", b.Name, b.InstallHint))
			}
		}
	}

	if len(set.Python) > 0 {
		provisioner, ok := ProvisionerFor("python")
		if !ok {
			warnings = append(warnings, "no python provisioner registered; python dependencies left unprovisioned")
			return resolved, warnings, nil
		}
		union, err := s.enabledPythonRequirements(ctx, workspaceID, set)
		if err != nil {
			return resolved, warnings, err
		}
		if provision {
			statuses, err := provisioner.Provision(ctx, ProvisionRequest{WorkspaceID: workspaceID, SkillsRoot: skillsRoot, Requirements: union})
			if err != nil {
				return resolved, warnings, err
			}
			resolved.Python = statuses
		} else {
			statuses, err := provisioner.Check(ctx, CheckRequest{WorkspaceID: workspaceID, SkillsRoot: skillsRoot, Requirements: union})
			if err != nil {
				return resolved, warnings, err
			}
			resolved.Python = statuses
		}
		for _, p := range resolved.Python {
			if p.Status == DepUnprovisioned {
				warnings = append(warnings, fmt.Sprintf("python dependency %q is not provisioned yet", p.Requirement))
				break
			}
		}
	}
	return resolved, warnings, nil
}

// enabledPythonRequirements unions the enabled skills' python requirements
// (including the freshly installed set being resolved).
func (s *InstallService) enabledPythonRequirements(ctx context.Context, workspaceID string, current DependencySet) ([]SkillRequirements, error) {
	enabled, err := s.skills.ListEnabled(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	perSkill := make([]SkillRequirements, 0, len(enabled)+1)
	for _, row := range enabled {
		reqs := row.Dependencies.PythonRequirements()
		if len(reqs) == 0 {
			continue
		}
		perSkill = append(perSkill, SkillRequirements{Skill: row.Name, Requirements: reqs})
	}
	if reqs := current.PythonRequirements(); len(reqs) > 0 {
		perSkill = append(perSkill, SkillRequirements{Skill: "(installing)", Requirements: reqs})
	}
	return perSkill, nil
}

// toolStatuses resolves tool names against the workspace tool gate and
// every agent allowlist: met only when the gate allows the tool and each
// agent's allowlist carries it (design D5; agent-tier installs scope the
// allowlist check to the owning agent at the handler seam).
func (s *InstallService) toolStatuses(ctx context.Context, workspaceID string, names []string) ([]ToolDependency, error) {
	rows, err := s.toolSettings.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	gateDisabled := map[string]bool{}
	for _, row := range rows {
		if !row.Enabled {
			gateDisabled[row.ToolKey] = true
		}
	}
	agents, err := s.agents.ListForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	statuses := make([]ToolDependency, 0, len(names))
	for _, name := range names {
		status := ToolDependency{Name: name, Status: DepMet}
		if gateDisabled[name] {
			status.Status = DepMissing
		}
		for i := range agents {
			if !hasAllTools(agents[i].Tools, []string{name}) {
				status.Status = DepMissing
				break
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// enableToolInGate writes an enabled row for the tool, preserving stored
// config (the gate only records explicit overrides).
func enableToolInGate(ctx context.Context, settings store.ToolSettingsStore, workspaceID, tool string) error {
	existing, err := settings.Get(ctx, workspaceID, tool)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if existing != nil && existing.Enabled {
		return nil
	}
	setting := &domain.WorkspaceToolSetting{WorkspaceID: workspaceID, ToolKey: tool, Enabled: true}
	if existing != nil {
		setting.Config = existing.Config
	}
	return settings.Upsert(ctx, setting)
}

func hasAllTools(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, t := range have {
		set[t] = true
	}
	for _, t := range want {
		if !set[t] {
			return false
		}
	}
	return true
}

func mergeTools(have, add []string) []string {
	set := make(map[string]bool, len(have))
	for _, t := range have {
		set[t] = true
	}
	merged := append([]string(nil), have...)
	for _, t := range add {
		if !set[t] {
			set[t] = true
			merged = append(merged, t)
		}
	}
	return merged
}

// validateStagedFiles re-checks staged paths (defense in depth ahead of
// the disk write): relative, clean, no traversal, no absolute paths.
func validateStagedFiles(files []StagedFile) error {
	for _, file := range files {
		if file.Path == "" || path.IsAbs(file.Path) || path.Clean(file.Path) != file.Path {
			return fmt.Errorf("%w: staged file path %q is invalid", domain.ErrInvalid, file.Path)
		}
		if strings.HasPrefix(file.Path, "../") || file.Path == ".." {
			return fmt.Errorf("%w: staged file path %q escapes the skill directory", domain.ErrInvalid, file.Path)
		}
	}
	return nil
}

// writeStaged writes files into a staging dot-directory and swaps it into
// place atomically (rename), replacing any prior tree.
func writeStaged(skillsRoot string, staged *StagedSkill) error {
	staging := filepath.Join(skillsRoot, "."+staged.Name+".staging")
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear staging dir: %w", err)
	}
	for _, file := range staged.Files {
		target := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create staging dirs: %w", err)
		}
		mode := os.FileMode(file.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(target, file.Content, mode); err != nil {
			return fmt.Errorf("write staged file %q: %w", file.Path, err)
		}
	}
	final := filepath.Join(skillsRoot, staged.Name)
	if err := os.RemoveAll(final); err != nil {
		return fmt.Errorf("replace skill dir: %w", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("swap skill dir into place: %w", err)
	}
	return nil
}

// bumpPatch increments the patch segment of a semver-ish version;
// unparseable versions restart at AuthorVersion with a bumped patch.
func bumpPatch(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "0.1.1"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "0.1.1"
	}
	parts[2] = strconv.Itoa(patch + 1)
	return strings.Join(parts, ".")
}
