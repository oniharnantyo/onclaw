package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	estore "github.com/oniharnantyo/onclaw/internal/store/fake"
)

type failingCreateStore struct {
	Store
}

func (f *failingCreateStore) Create(ctx context.Context, skill *Skill) error {
	return errors.New("simulated row-insert failure")
}

func setupService(t *testing.T, skillStore Store, opts ...InstallOption) (*InstallService, string, string) {
	t.Helper()
	st := estore.New()
	ctx := context.Background()
	ws := &domain.Workspace{Name: "Acme", Slug: "acme"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	provider := &domain.ProviderConfig{WorkspaceID: ws.ID, Type: "openai", Name: "primary", Enabled: true}
	if err := st.Providers().Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	agent := &domain.Agent{WorkspaceID: ws.ID, Name: "Atlas", Slug: "atlas", ProviderID: provider.ID, Model: "gpt-x", Tools: []string{"ls"}}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	onClawDir := t.TempDir()
	opts = append([]InstallOption{WithCommandRunner(&fakeRunner{})}, opts...)
	svc := NewInstallService(skillStore, st.Agents(), st.ToolSettings(), opts...)
	return svc, ws.ID, onClawDir
}

func installAuthor(t *testing.T, svc *InstallService, wsID, dir, name string, overwrite bool) ([]*InstallResult, error) {
	t.Helper()
	return svc.Install(context.Background(), InstallInput{
		WorkspaceID: wsID,
		TenantSlug:  "acme",
		OnClawDir:   dir,
		Source:      SourceAuthored,
		Name:        name,
		Description: "Sweeps changelogs",
		Body:        "# Changelog sweeper\n\nUse web.search to find releases.",
		Overwrite:   overwrite,
	})
}

// stAgents returns the workspace's agents keyed by name.
func stAgents(t *testing.T, svc *InstallService, wsID string) map[string]domain.Agent {
	t.Helper()
	agents, err := svc.agents.ListForWorkspace(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]domain.Agent{}
	for _, a := range agents {
		out[a.Name] = a
	}
	return out
}

func TestInstallAuthorCreatesRowAndFiles(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())

	results, err := installAuthor(t, svc, wsID, dir, "changelog-sweeper", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	skill := results[0].Skill
	if skill.Name != "changelog-sweeper" || skill.Version != "0.1.0" || skill.Source != SourceAuthored || !skill.Enabled {
		t.Errorf("row = %+v", skill)
	}

	body, err := os.ReadFile(filepath.Join(domain.WorkspaceSkillsDir(dir, "acme"), "changelog-sweeper", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill body missing on disk: %v", err)
	}
	if !strings.Contains(string(body), "Changelog sweeper") {
		t.Errorf("SKILL.md body wrong: %q", body)
	}
	if !strings.Contains(string(body), "version: 0.1.0") {
		t.Errorf("SKILL.md must carry version 0.1.0: %q", body)
	}

	// Inference: body mentions web.search; stored on the row, files untouched.
	found := false
	for _, dep := range skill.Dependencies.Tools {
		if dep.Name == "web.search" {
			found = true
		}
	}
	if !found {
		t.Errorf("web.search tool dependency not stored: %+v", skill.Dependencies)
	}
}

func TestInstallSlugCollisionRequiresConfirm(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())
	if _, err := installAuthor(t, svc, wsID, dir, "changelog-sweeper", false); err != nil {
		t.Fatal(err)
	}

	_, err := installAuthor(t, svc, wsID, dir, "changelog-sweeper", false)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict without confirm, got %v", err)
	}

	results, err := installAuthor(t, svc, wsID, dir, "changelog-sweeper", true)
	if err != nil {
		t.Fatalf("overwrite install: %v", err)
	}
	if results[0].Skill.Version != "0.1.1" {
		t.Errorf("overwrite must bump the version (0.1.0 -> 0.1.1), got %q", results[0].Skill.Version)
	}
	// Enabled state survives the overwrite.
	if !results[0].Skill.Enabled {
		t.Errorf("overwrite must preserve the enabled flag")
	}
}

func TestInstallRowFailureRemovesWrittenTree(t *testing.T) {
	svc, wsID, dir := setupService(t, &failingCreateStore{Store: NewMemoryStore()})

	_, err := installAuthor(t, svc, wsID, dir, "orphan-check", false)
	if err == nil {
		t.Fatal("expected row-insert failure to surface")
	}
	skillDir := filepath.Join(domain.WorkspaceSkillsDir(dir, "acme"), "orphan-check")
	if _, statErr := os.Stat(skillDir); !os.IsNotExist(statErr) {
		t.Errorf("orphan tree must be removed on row failure, stat err = %v", statErr)
	}
}

func TestInstallRejectsInvalidNames(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())
	// "api" is on the shared reserved list; "Bad Name!!" slugifies to a
	// valid name instead (names derive by slugification, spec D4).
	if _, err := installAuthor(t, svc, wsID, dir, "api", false); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("reserved name must be rejected, got %v", err)
	}
	if _, err := installAuthor(t, svc, wsID, dir, "Bad Name!!", false); err != nil {
		t.Errorf("slugifiable name must be accepted, got %v", err)
	}
}

func TestInstallForkCopiesSystemSkill(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())
	results, err := svc.Install(context.Background(), InstallInput{
		WorkspaceID: wsID,
		TenantSlug:  "acme",
		OnClawDir:   dir,
		Source:      SourceFork,
		SystemSkill: "web-research",
	})
	if err != nil {
		t.Fatalf("Install fork: %v", err)
	}
	skill := results[0].Skill
	if skill.Source != SourceFork || skill.Name != "web-research" {
		t.Errorf("fork row = %+v", skill)
	}
	if _, err := os.Stat(filepath.Join(domain.WorkspaceSkillsDir(dir, "acme"), "web-research", "SKILL.md")); err != nil {
		t.Fatalf("forked body missing: %v", err)
	}

	// Unknown system skill is invalid.
	if _, err := svc.Install(context.Background(), InstallInput{
		WorkspaceID: wsID, TenantSlug: "acme", OnClawDir: dir,
		Source: SourceFork, SystemSkill: "no-such-system-skill",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown system skill must be invalid, got %v", err)
	}
}

func TestInstallUploadZip(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())
	archive := buildZip(t, []struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}{
		{Name: "SKILL.md", Mode: 0o644, Content: []byte("---\nname: uploaded\ndescription: uploaded skill\n---\nUses execute.\n")},
		{Name: "scripts/run.sh", Mode: 0o755, Content: []byte("#!/bin/sh")},
	})
	results, err := svc.Install(context.Background(), InstallInput{
		WorkspaceID: wsID, TenantSlug: "acme", OnClawDir: dir,
		Source: SourceUpload, Archive: archive,
	})
	if err != nil {
		t.Fatalf("Install upload: %v", err)
	}
	if results[0].Skill.Name != "uploaded" {
		t.Errorf("name = %q (from frontmatter)", results[0].Skill.Name)
	}
	// scripts/ presence implies execute.
	names := []string{}
	for _, dep := range results[0].Skill.Dependencies.Tools {
		names = append(names, dep.Name)
	}
	if !slices.Contains(names, "execute") {
		t.Errorf("execute not inferred: %+v", results[0].Skill.Dependencies)
	}
}

func TestEnableEverywhereWritesGateAndAllowlists(t *testing.T) {
	memory := NewMemoryStore()
	var txSeen bool
	var svc *InstallService
	var wsID string
	svc, wsID, _ = setupService(t, memory, WithTxProvider(func(ctx context.Context, fn func(ctx context.Context, tx TxStores) error) error {
		txSeen = true
		return fn(ctx, TxStores{Skills: memory, Agents: svc.agents, ToolSettings: svc.toolSettings})
	}))

	if err := svc.EnableEverywhere(context.Background(), wsID, []string{"web.search"}); err != nil {
		t.Fatalf("EnableEverywhere: %v", err)
	}
	if !txSeen {
		t.Errorf("transaction seam must be used")
	}

	setting, err := svc.toolSettings.Get(context.Background(), wsID, "web.search")
	if err != nil {
		t.Fatalf("gate row missing: %v", err)
	}
	if !setting.Enabled {
		t.Errorf("gate row not enabled")
	}
	for name, agent := range stAgents(t, svc, wsID) {
		if !slices.Contains(agent.Tools, "web.search") {
			t.Errorf("agent %s allowlist missing web.search: %v", name, agent.Tools)
		}
	}
}

func TestToolStatusesRespectGateAndAllowlists(t *testing.T) {
	svc, wsID, _ := setupService(t, NewMemoryStore())
	ctx := context.Background()

	// Gate allows (no row) but the agent allowlist lacks web.search.
	statuses, err := svc.toolStatuses(ctx, wsID, []string{"web.search", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Status != DepMissing || statuses[1].Status != DepMet {
		t.Errorf("statuses = %+v", statuses)
	}

	// Disabling in the gate flips ls to missing even though the allowlist has it.
	if err := svc.toolSettings.Upsert(ctx, &domain.WorkspaceToolSetting{WorkspaceID: wsID, ToolKey: "ls", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	statuses, err = svc.toolStatuses(ctx, wsID, []string{"ls"})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Status != DepMissing {
		t.Errorf("gate-disabled tool must be missing: %+v", statuses)
	}
}

func TestInstallWithEnableEverywhereSatisfiesToolDeps(t *testing.T) {
	svc, wsID, dir := setupService(t, NewMemoryStore())
	ctx := context.Background()

	results, err := svc.Install(ctx, InstallInput{
		WorkspaceID: wsID, TenantSlug: "acme", OnClawDir: dir,
		Source: SourceAuthored, Name: "searcher",
		Description: "searches", Body: "Uses web.search heavily.",
		EnableEverywhere: true,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, dep := range results[0].Skill.Dependencies.Tools {
		if dep.Name == "web.search" && dep.Status != DepMet {
			t.Errorf("enable-everywhere must satisfy web.search: %+v", dep)
		}
	}
	for name, agent := range stAgents(t, svc, wsID) {
		if !slices.Contains(agent.Tools, "web.search") {
			t.Errorf("agent %s allowlist not updated", name)
		}
	}
}

func TestUninstallRemovesRowAndTree(t *testing.T) {
	memory := NewMemoryStore()
	svc, wsID, dir := setupService(t, memory)
	ctx := context.Background()
	if _, err := installAuthor(t, svc, wsID, dir, "changelog-sweeper", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Uninstall(ctx, wsID, "acme", dir, "changelog-sweeper"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := memory.Get(ctx, wsID, "changelog-sweeper"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("row survives uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(domain.WorkspaceSkillsDir(dir, "acme"), "changelog-sweeper")); !os.IsNotExist(err) {
		t.Errorf("tree survives uninstall")
	}
}

func TestRecheckDependenciesRefreshesBinaryStatus(t *testing.T) {
	memory := NewMemoryStore()
	runner := &fakeRunner{lookPath: map[string]string{"sh": "/bin/sh"}}
	svc, wsID, dir := setupService(t, memory, WithCommandRunner(runner))
	ctx := context.Background()

	// Install an uploaded skill declaring a binary that is absent.
	archive := buildZip(t, []struct {
		Name    string
		Mode    os.FileMode
		Content []byte
	}{
		{Name: "SKILL.md", Mode: 0o644, Content: []byte("---\nname: pdf-splitter\ndescription: splits\ndependencies:\n  binaries:\n    - pdftotext\n---\nsplits pdfs")},
	})
	results, err := svc.Install(ctx, InstallInput{
		WorkspaceID: wsID, TenantSlug: "acme", OnClawDir: dir,
		Source: SourceUpload, Archive: archive,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	found := false
	for _, dep := range results[0].Skill.Dependencies.Binaries {
		if dep.Name == "pdftotext" {
			found = true
			if dep.Status != DepMissing || dep.InstallHint == "" {
				t.Errorf("binary dep = %+v", dep)
			}
		}
	}
	if !found {
		t.Fatalf("frontmatter binary declaration not inferred: %+v", results[0].Skill.Dependencies)
	}

	// The binary appears on the PATH; re-check flips the row to met.
	runner.lookPath["pdftotext"] = "/usr/local/bin/pdftotext"
	row, err := svc.RecheckDependencies(ctx, wsID, "acme", dir, "pdf-splitter")
	if err != nil {
		t.Fatalf("RecheckDependencies: %v", err)
	}
	for _, dep := range row.Dependencies.Binaries {
		if dep.Name == "pdftotext" && dep.Status != DepMet {
			t.Errorf("re-check did not update the row: %+v", dep)
		}
	}
}
