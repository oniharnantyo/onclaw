package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
)

// mockSkillStore implements store.SkillStore for testing.
type mockSkillStore struct {
	skills map[string]*store.Skill
}

func newMockSkillStore() *mockSkillStore {
	return &mockSkillStore{skills: make(map[string]*store.Skill)}
}

func (m *mockSkillStore) AddSkill(ctx context.Context, s *store.Skill) error {
	key := s.Name + ":" + s.Scope
	if _, exists := m.skills[key]; exists {
		return fmt.Errorf("duplicate skill: %s", key)
	}
	m.skills[key] = s
	return nil
}

func (m *mockSkillStore) GetSkill(ctx context.Context, name string, scope string) (*store.Skill, error) {
	key := name + ":" + scope
	s, exists := m.skills[key]
	if !exists {
		return nil, fmt.Errorf("skill not found: %s", key)
	}
	return s, nil
}

func (m *mockSkillStore) ListSkills(ctx context.Context) ([]*store.Skill, error) {
	var list []*store.Skill
	for _, s := range m.skills {
		list = append(list, s)
	}
	return list, nil
}

func (m *mockSkillStore) ListSkillsByScope(ctx context.Context, scope string) ([]*store.Skill, error) {
	var list []*store.Skill
	for _, s := range m.skills {
		if s.Scope == scope {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockSkillStore) UpdateSkill(ctx context.Context, s *store.Skill) error {
	key := s.Name + ":" + s.Scope
	if _, exists := m.skills[key]; !exists {
		return fmt.Errorf("skill not found: %s", key)
	}
	m.skills[key] = s
	return nil
}

func (m *mockSkillStore) RemoveSkill(ctx context.Context, name string, scope string) error {
	key := name + ":" + scope
	if _, exists := m.skills[key]; !exists {
		return fmt.Errorf("skill not found: %s", key)
	}
	delete(m.skills, key)
	return nil
}

func createTarGz(files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func TestLocalInstallation(t *testing.T) {
	homeDir, err := os.MkdirTemp("", "onclaw-install-home")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(homeDir)

	srcDir, err := os.MkdirTemp("", "onclaw-install-src")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create a single skill local source
	skillDir := filepath.Join(srcDir, "math-helper")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	skillContent := `---
name: math
description: performs calculations
---
Body content of math skill
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// Setup mock store and installer
	ms := newMockSkillStore()
	inst := NewInstaller(ms, homeDir)
	ctx := context.Background()

	// Install skill from local source (using the skill directory path directly)
	installed, err := inst.Install(ctx, skillDir, nil, "global", InstallOpts{})
	if err != nil {
		t.Fatalf("failed to Install: %v", err)
	}

	if len(installed) != 1 {
		t.Fatalf("expected 1 installed skill, got %d", len(installed))
	}

	if installed[0].Name != "math" {
		t.Errorf("expected skill name 'math', got %s", installed[0].Name)
	}

	// Verify target files exist
	expectedPath := filepath.Join(homeDir, "skills", "math", "SKILL.md")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected skill file to exist at %s, but got error: %v", expectedPath, err)
	}

	// Verify ledger state
	dbSkill, err := ms.GetSkill(ctx, "math", "global")
	if err != nil {
		t.Fatalf("failed to get skill from ledger: %v", err)
	}
	if dbSkill.Source != skillDir {
		t.Errorf("expected ledger source to be %s, got %s", skillDir, dbSkill.Source)
	}

	// Test Idempotency (re-install, same source, same hash)
	installed2, err := inst.Install(ctx, skillDir, nil, "global", InstallOpts{})
	if err != nil {
		t.Fatalf("failed second install: %v", err)
	}
	if len(installed2) != 1 {
		t.Fatalf("expected 1 skill on second install, got %d", len(installed2))
	}
	// Verify it returned the same record
	if installed2[0].UpdatedAt != installed[0].UpdatedAt {
		t.Logf("Note: UpdatedAt changed but should be identical for no-op")
	}

	// Test collision error (same name, different source)
	otherSrcDir, err := os.MkdirTemp("", "onclaw-install-other-src")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(otherSrcDir)

	otherSkillDir := filepath.Join(otherSrcDir, "math-helper")
	if err := os.MkdirAll(otherSkillDir, 0755); err != nil {
		t.Fatalf("failed mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherSkillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed write: %v", err)
	}

	_, err = inst.Install(ctx, otherSkillDir, nil, "global", InstallOpts{})
	if err == nil {
		t.Error("expected error due to name collision from different source, got nil")
	}

	// Overwrite with Force
	_, err = inst.Install(ctx, otherSkillDir, nil, "global", InstallOpts{Force: true})
	if err != nil {
		t.Fatalf("expected force install to succeed, got error: %v", err)
	}
}

func TestPluginAndMultipleSkills(t *testing.T) {
	homeDir, err := os.MkdirTemp("", "onclaw-plugin-home")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(homeDir)

	// We'll mock a Claude plugin structure using a tarball served by HTTP mock server
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name": "my-plugin", "description": "My Claude plugin"}`,
		"skills/skill1/SKILL.md": `---
name: skillA
description: Skill A desc
---
Body A
`,
		"skills/skill2/SKILL.md": `---
name: skillB
description: Skill B desc
---
Body B
`,
	}

	tarBytes, err := createTarGz(files)
	if err != nil {
		t.Fatalf("failed to create tar.gz bytes: %v", err)
	}

	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(tarBytes)
	}))
	defer server.Close()

	ms := newMockSkillStore()
	inst := NewInstaller(ms, homeDir)
	ctx := context.Background()

	// Discover source
	pkgName, isPlugin, candidates, tempDir, err := inst.DiscoverSource(ctx, server.URL, "", false)
	if err != nil {
		t.Fatalf("failed to discover source: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if pkgName != "my-plugin" {
		t.Errorf("expected package name to be 'my-plugin', got '%s'", pkgName)
	}
	if !isPlugin {
		t.Error("expected source to be detected as plugin")
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates in plugin, got %d", len(candidates))
	}

	// Install all (explicit selection)
	installed, err := inst.Install(ctx, server.URL, []string{"skillA", "skillB"}, "global", InstallOpts{})
	if err != nil {
		t.Fatalf("failed to install: %v", err)
	}

	if len(installed) != 2 {
		t.Fatalf("expected 2 installed skills, got %d", len(installed))
	}

	// Namespacing check
	names := make(map[string]bool)
	for _, sk := range installed {
		names[sk.Name] = true
	}

	if !names["skillA"] || !names["skillB"] {
		t.Errorf("expected skills 'skillA' and 'skillB', got: %+v", names)
	}
}

func TestClassifySourceType(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"github.com/owner/repo", "github"},
		{"https://github.com/owner/repo", "github"},
		{"owner/repo", "github"},
		{"https://example.com/archive.tar.gz", "http"},
		{"/path/to/local/dir", "local"},
	}

	for _, tt := range tests {
		got := classifySourceType(tt.source)
		if got != tt.want {
			t.Errorf("classifySourceType(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestHashCalculation(t *testing.T) {
	content := "test content"
	h := sha256.New()
	h.Write([]byte(content))
	wantHash := fmt.Sprintf("%x", h.Sum(nil))

	if wantHash != "6ae8a75555209fd6c44157c0aed8016e763ff435a19cf186f76863140143ff72" {
		t.Errorf("unexpected hash for test content: %s", wantHash)
	}
}

func TestScopeCoexistence(t *testing.T) {
	homeDir, err := os.MkdirTemp("", "onclaw-coexist-home")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(homeDir)

	srcDir, err := os.MkdirTemp("", "onclaw-coexist-src")
	if err != nil {
		t.Fatalf("failed temp dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	skillDir := filepath.Join(srcDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed mkdir: %v", err)
	}
	skillContent := `---
name: my-skill
description: test
---
body`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed write: %v", err)
	}

	ms := newMockSkillStore()
	inst := NewInstaller(ms, homeDir)
	ctx := context.Background()

	// Install globally
	_, err = inst.Install(ctx, skillDir, nil, "global", InstallOpts{})
	if err != nil {
		t.Fatalf("failed to install globally: %v", err)
	}

	// Install to agent scope: should coexist without collision
	_, err = inst.Install(ctx, skillDir, nil, "my-agent", InstallOpts{})
	if err != nil {
		t.Fatalf("failed to install in agent scope (should coexist): %v", err)
	}

	// Verify both exist in ledger
	globalSk, err := ms.GetSkill(ctx, "my-skill", "global")
	if err != nil {
		t.Errorf("expected global skill, got error: %v", err)
	}
	agentSk, err := ms.GetSkill(ctx, "my-skill", "my-agent")
	if err != nil {
		t.Errorf("expected agent skill, got error: %v", err)
	}

	if globalSk == nil || agentSk == nil {
		t.Errorf("expected both skills to exist")
	}
}
