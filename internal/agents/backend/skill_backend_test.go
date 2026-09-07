package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agents/systemskills"
	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestSkillBackend_Precedence(t *testing.T) {
	ctx := context.Background()

	t.Run("agent overrides workspace and system", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create system skill
		systemDir := domain.SystemSkillsDir(tmpDir)
		createSkill(t, systemDir, "shared-skill", "System version")

		// Create workspace skill with same name
		workspaceDir := domain.WorkspaceSkillsDir(tmpDir, "test-tenant")
		createSkill(t, workspaceDir, "shared-skill", "Workspace version")

		// Create agent skill with same name
		agentDir := domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent")
		createSkill(t, agentDir, "shared-skill", "Agent version")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(metadata) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(metadata))
		}

		skill := metadata[0]
		if skill.Name != "shared-skill" {
			t.Errorf("expected skill name 'shared-skill', got %s", skill.Name)
		}
		if skill.Description != "Agent version" {
			t.Errorf("expected description 'Agent version', got %s", skill.Description)
		}
	})

	t.Run("agent overrides workspace", func(t *testing.T) {
		tmpDir := t.TempDir()

		workspaceDir := domain.WorkspaceSkillsDir(tmpDir, "test-tenant")
		createSkill(t, workspaceDir, "workspace-only", "Workspace version")

		agentDir := domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent")
		createSkill(t, agentDir, "workspace-only", "Agent version")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(metadata) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(metadata))
		}

		body, err := resolver.Get(ctx, "workspace-only")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !contains(body.Content, "Agent version") {
			t.Errorf("expected agent tier to win, got %s", body.Content)
		}
	})

	t.Run("workspace overrides system", func(t *testing.T) {
		tmpDir := t.TempDir()

		systemDir := domain.SystemSkillsDir(tmpDir)
		createSkill(t, systemDir, "system-workspace", "System version")

		workspaceDir := domain.WorkspaceSkillsDir(tmpDir, "test-tenant")
		createSkill(t, workspaceDir, "system-workspace", "Workspace version")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(metadata) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(metadata))
		}

		body, err := resolver.Get(ctx, "system-workspace")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !contains(body.Content, "Workspace version") {
			t.Errorf("expected workspace tier to win, got %s", body.Content)
		}
	})
}

func TestSkillBackend_EnabledWorkspaceSkills(t *testing.T) {
	ctx := context.Background()

	t.Run("only enabled workspace skills attach", func(t *testing.T) {
		tmpDir := t.TempDir()

		// System and agent skills are always present.
		createSkill(t, domain.SystemSkillsDir(tmpDir), "system-skill", "System skill")
		createSkill(t, domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent"), "agent-skill", "Agent skill")

		// Two workspace skills, only one enabled in the registry.
		workspaceDir := domain.WorkspaceSkillsDir(tmpDir, "test-tenant")
		createSkill(t, workspaceDir, "enabled-skill", "Enabled skill")
		createSkill(t, workspaceDir, "disabled-skill", "Disabled skill")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", staticEnabledReader{"enabled-skill"})

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(metadata) != 3 {
			t.Fatalf("expected 3 skills (system + agent + enabled workspace), got %d", len(metadata))
		}

		names := make(map[string]bool, len(metadata))
		for _, m := range metadata {
			names[m.Name] = true
		}
		if names["disabled-skill"] {
			t.Error("disabled workspace skill must not attach")
		}
		if !names["system-skill"] || !names["agent-skill"] || !names["enabled-skill"] {
			t.Errorf("expected system-skill, agent-skill, enabled-skill; got %v", names)
		}
	})

	t.Run("master switch off: workspace tier absent", func(t *testing.T) {
		tmpDir := t.TempDir()

		createSkill(t, domain.SystemSkillsDir(tmpDir), "system-skill", "System skill")
		createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "ws-skill", "Workspace skill")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", staticEnabledReader{})

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(metadata) != 1 || metadata[0].Name != "system-skill" {
			t.Fatalf("expected only system-skill, got %v", metadata)
		}

		if _, err := resolver.Get(ctx, "ws-skill"); err == nil {
			t.Error("Get must reject a disabled workspace skill")
		}
	})

	t.Run("disabled workspace collision falls through to system", func(t *testing.T) {
		tmpDir := t.TempDir()

		createSkill(t, domain.SystemSkillsDir(tmpDir), "shared", "System version")
		createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "shared", "Workspace version")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", staticEnabledReader{})

		body, err := resolver.Get(ctx, "shared")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !contains(body.Content, "System version") {
			t.Errorf("expected system tier to win when workspace skill is disabled, got %s", body.Content)
		}
	})

	t.Run("nil reader treats all workspace skills as enabled", func(t *testing.T) {
		tmpDir := t.TempDir()

		createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "ws-skill", "Workspace skill")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)

		metadata, err := resolver.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(metadata) != 1 || metadata[0].Name != "ws-skill" {
			t.Fatalf("expected ws-skill with nil reader, got %v", metadata)
		}
	})

	t.Run("reader error surfaces from List", func(t *testing.T) {
		tmpDir := t.TempDir()
		createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "ws-skill", "Workspace skill")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", errorReader{})

		if _, err := resolver.List(ctx); err == nil {
			t.Error("expected List to fail when the enabled-reader errors")
		}
	})
}

func TestAvailableSkillNames(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	createSkill(t, domain.SystemSkillsDir(tmpDir), "sys-skill", "System skill")
	createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "ws-enabled", "Enabled")
	createSkill(t, domain.WorkspaceSkillsDir(tmpDir, "test-tenant"), "ws-disabled", "Disabled")
	createSkill(t, domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent"), "agent-skill", "Agent skill")

	names, err := AvailableSkillNames(ctx, tmpDir, "test-tenant", "test-agent", staticEnabledReader{"ws-enabled"})
	if err != nil {
		t.Fatalf("AvailableSkillNames: %v", err)
	}

	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	for _, want := range []string{"sys-skill", "ws-enabled", "agent-skill"} {
		if !set[want] {
			t.Errorf("expected %q in available names, got %v", want, names)
		}
	}
	if set["ws-disabled"] {
		t.Errorf("disabled workspace skill must not be available, got %v", names)
	}
}

func TestParseFrontmatter_Dependencies(t *testing.T) {
	t.Run("declared dependencies", func(t *testing.T) {
		content := `---
name: pdf-toolkit
description: PDF tools
dependencies:
  tools:
    - execute
    - files.write
  binaries:
    - qpdf
  python:
    - reportlab
    - pypdf
---
# PDF toolkit
Body text.
`
		description, deps := parseFrontmatter(content)
		if description != "PDF tools" {
			t.Errorf("description = %q, want %q", description, "PDF tools")
		}
		if len(deps.Tools) != 2 || deps.Tools[0] != "execute" || deps.Tools[1] != "files.write" {
			t.Errorf("Tools = %v, want [execute files.write]", deps.Tools)
		}
		if len(deps.Binaries) != 1 || deps.Binaries[0] != "qpdf" {
			t.Errorf("Binaries = %v, want [qpdf]", deps.Binaries)
		}
		if len(deps.Python) != 2 || deps.Python[0] != "reportlab" || deps.Python[1] != "pypdf" {
			t.Errorf("Python = %v, want [reportlab pypdf]", deps.Python)
		}
	})

	t.Run("inline flow style", func(t *testing.T) {
		content := "---\ndescription: Inline deps\ndependencies: {tools: [execute], binaries: [], python: [rich]}\n---\nbody"
		_, deps := parseFrontmatter(content)
		if len(deps.Tools) != 1 || deps.Tools[0] != "execute" {
			t.Errorf("Tools = %v, want [execute]", deps.Tools)
		}
		if len(deps.Binaries) != 0 {
			t.Errorf("Binaries = %v, want empty", deps.Binaries)
		}
		if len(deps.Python) != 1 || deps.Python[0] != "rich" {
			t.Errorf("Python = %v, want [rich]", deps.Python)
		}
	})

	t.Run("no dependencies declared", func(t *testing.T) {
		content := "---\nname: plain\ndescription: No deps\n---\n# Plain\n"
		description, deps := parseFrontmatter(content)
		if description != "No deps" {
			t.Errorf("description = %q, want %q", description, "No deps")
		}
		if len(deps.Tools)+len(deps.Binaries)+len(deps.Python) != 0 {
			t.Errorf("expected zero dependencies, got %+v", deps)
		}
	})

	t.Run("malformed frontmatter degrades gracefully", func(t *testing.T) {
		content := "---\ndescription: Broken\ndependencies:\n  tools: [unclosed\n    binaries:\n      - not-a-real-key-list\npython:\nnot frontmatter at all"
		description, deps := parseFrontmatter(content)
		if description == "" {
			t.Error("expected a description despite malformed frontmatter")
		}
		// Whatever parses must never panic; lists stay well-formed.
		for _, list := range [][]string{deps.Tools, deps.Binaries, deps.Python} {
			for _, item := range list {
				if item == "" {
					t.Error("empty dependency item parsed")
				}
			}
		}
	})

	t.Run("unterminated frontmatter treated as body", func(t *testing.T) {
		content := "description: body fallback\nno closing marker"
		description, deps := parseFrontmatter(content)
		if description != "body fallback" {
			t.Errorf("description = %q, want body fallback", description)
		}
		if len(deps.Tools)+len(deps.Binaries)+len(deps.Python) != 0 {
			t.Errorf("expected zero dependencies, got %+v", deps)
		}
	})
}

// staticEnabledReader is a fixed EnabledSkillReader fake.
type staticEnabledReader []string

func (s staticEnabledReader) EnabledSkillNames(ctx context.Context, workspaceSlug string) ([]string, error) {
	return append([]string(nil), s...), nil
}

// errorReader always fails, to exercise error propagation.
type errorReader struct{}

func (errorReader) EnabledSkillNames(ctx context.Context, workspaceSlug string) ([]string, error) {
	return nil, fmt.Errorf("registry unavailable")
}

func TestSkillBackend_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("get skill from agent tier", func(t *testing.T) {
		tmpDir := t.TempDir()

		agentDir := domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent")
		createSkill(t, agentDir, "agent-skill", "Agent skill content")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		body, err := resolver.Get(ctx, "agent-skill")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if body.Name != "agent-skill" {
			t.Errorf("expected name 'agent-skill', got %s", body.Name)
		}
		if !contains(body.Content, "Agent skill content") {
			t.Errorf("expected content to contain 'Agent skill content', got %s", body.Content)
		}
	})

	t.Run("get skill with precedence", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create skill in all tiers
		systemDir := domain.SystemSkillsDir(tmpDir)
		createSkill(t, systemDir, "shared", "System version")

		workspaceDir := domain.WorkspaceSkillsDir(tmpDir, "test-tenant")
		createSkill(t, workspaceDir, "shared", "Workspace version")

		agentDir := domain.AgentSkillsDir(tmpDir, "test-tenant", "test-agent")
		createSkill(t, agentDir, "shared", "Agent version")

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		body, err := resolver.Get(ctx, "shared")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if !contains(body.Content, "Agent version") {
			t.Errorf("expected agent tier to win, got %s", body.Content)
		}
	})

	t.Run("get non-existent skill returns error", func(t *testing.T) {
		tmpDir := t.TempDir()

		resolver := NewSkillBackend(tmpDir, "test-tenant", "test-agent", nil)
		_, err := resolver.Get(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent skill, got nil")
		}
	})
}

func TestSystemSkillsSync(t *testing.T) {
	t.Run("sync idempotence", func(t *testing.T) {
		tmpDir := t.TempDir()

		// First sync
		err := systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("First sync failed: %v", err)
		}

		// Verify files were created
		embedded, err := systemskills.ListEmbedded()
		if err != nil {
			t.Fatalf("ListEmbedded failed: %v", err)
		}

		for _, name := range embedded {
			skillPath := filepath.Join(tmpDir, name, "SKILL.md")
			if _, err := os.Stat(skillPath); err != nil {
				t.Errorf("Expected skill file %s to exist after first sync", skillPath)
			}
		}

		// Second sync should be no-op
		err = systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("Second sync failed: %v", err)
		}

		// Verify files still exist and content matches
		for _, name := range embedded {
			matches, err := systemskills.VerifyChecksum(name, tmpDir)
			if err != nil {
				t.Errorf("VerifyChecksum failed for %s: %v", name, err)
			}
			if !matches {
				t.Errorf("Expected content to match after second sync for skill %s", name)
			}
		}
	})

	t.Run("extraneous file removal", func(t *testing.T) {
		tmpDir := t.TempDir()

		// First sync
		err := systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("First sync failed: %v", err)
		}

		// Create an extraneous skill directory
		extraDir := filepath.Join(tmpDir, "extra-skill")
		if err := os.MkdirAll(extraDir, 0755); err != nil {
			t.Fatalf("Failed to create extra directory: %v", err)
		}
		extraFile := filepath.Join(extraDir, "SKILL.md")
		if err := os.WriteFile(extraFile, []byte("extra content"), 0644); err != nil {
			t.Fatalf("Failed to write extra file: %v", err)
		}

		// Second sync should remove extraneous files
		err = systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("Second sync failed: %v", err)
		}

		// Verify extraneous file was removed
		if _, err := os.Stat(extraDir); err == nil {
			t.Errorf("Expected extraneous directory to be removed, but it still exists")
		}
	})

	t.Run("overwrite when content differs", func(t *testing.T) {
		tmpDir := t.TempDir()

		// First sync
		err := systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("First sync failed: %v", err)
		}

		// Modify a skill file
		embedded, err := systemskills.ListEmbedded()
		if err != nil {
			t.Fatalf("ListEmbedded failed: %v", err)
		}

		if len(embedded) == 0 {
			t.Fatal("No embedded skills found")
		}

		modifiedPath := filepath.Join(tmpDir, embedded[0], "SKILL.md")
		if err := os.WriteFile(modifiedPath, []byte("modified content"), 0644); err != nil {
			t.Fatalf("Failed to modify skill file: %v", err)
		}

		// Second sync should restore original content
		err = systemskills.SyncSystemSkills(tmpDir)
		if err != nil {
			t.Fatalf("Second sync failed: %v", err)
		}

		// Verify content was restored
		content, err := os.ReadFile(modifiedPath)
		if err != nil {
			t.Fatalf("Failed to read skill file: %v", err)
		}

		if contains(string(content), "modified content") {
			t.Errorf("Expected original content to be restored, but found modified content")
		}
	})
}

// Helper functions

func createSkill(t *testing.T, dir, name, description string) {
	t.Helper()

	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}

	skillFile := filepath.Join(skillDir, "SKILL.md")
	content := `# ` + name + `

name: ` + name + `
description: ` + description + `

This is a test skill.
`
	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
