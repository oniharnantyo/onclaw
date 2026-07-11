package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/agent"
)

func TestGetTemplate(t *testing.T) {
	// Verify we can read standard templates
	content, err := agent.GetTemplate("IDENTITY.md")
	if err != nil {
		t.Fatalf("failed to read IDENTITY.md template: %v", err)
	}
	if !strings.Contains(content, "# IDENTITY") {
		t.Errorf("expected IDENTITY.md template to contain '# IDENTITY', got: %s", content)
	}

	// Verify non-existent template returns error
	_, err = agent.GetTemplate("nonexistent.md")
	if err == nil {
		t.Error("expected error reading nonexistent template, got nil")
	}
}

func TestSeedWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")

	// 1. Fresh seed
	err := agent.SeedWorkspace(workspace)
	if err != nil {
		t.Fatalf("SeedWorkspace failed: %v", err)
	}

	// Verify files exist and match templates
	expectedFiles := []string{
		"IDENTITY.md",
		"SOUL.md",
		"CAPABILITIES.md",
		"TOOLS.md",
		"USER.md",
		"MEMORY.md",
		"AGENTS.md",
	}

	for _, name := range expectedFiles {
		path := filepath.Join(workspace, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to be seeded, but it does not exist", name)
		}
	}

	// Verify content of one of the files matches
	agentsContent, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	tpl, _ := agent.GetTemplate("AGENTS.md")
	if string(agentsContent) != tpl {
		t.Errorf("AGENTS.md content does not match template. Got: %q, Expected: %q", string(agentsContent), tpl)
	}

	// 2. Non-destructive check: edit a file and re-seed
	customContent := "custom identity details"
	err = os.WriteFile(filepath.Join(workspace, "IDENTITY.md"), []byte(customContent), 0644)
	if err != nil {
		t.Fatalf("failed to write custom identity: %v", err)
	}

	err = agent.SeedWorkspace(workspace)
	if err != nil {
		t.Fatalf("SeedWorkspace on populated workspace failed: %v", err)
	}

	// Verify custom content was preserved
	identityContent, err := os.ReadFile(filepath.Join(workspace, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("failed to read IDENTITY.md: %v", err)
	}
	if string(identityContent) != customContent {
		t.Errorf("IDENTITY.md was overwritten during re-seed. Got: %q, Expected: %q", string(identityContent), customContent)
	}
}

func TestSeedGlobalUser(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".onclaw")

	// 1. Fresh seed
	err := agent.SeedGlobalUser(configDir)
	if err != nil {
		t.Fatalf("SeedGlobalUser failed: %v", err)
	}

	path := filepath.Join(configDir, "USER.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected global USER.md to be seeded, but it does not exist")
	}

	userContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read global USER.md: %v", err)
	}
	tpl, _ := agent.GetTemplate("USER.md")
	if string(userContent) != tpl {
		t.Errorf("global USER.md content does not match template. Got: %q, Expected: %q", string(userContent), tpl)
	}

	// 2. Non-destructive check
	customContent := "custom global user facts"
	err = os.WriteFile(path, []byte(customContent), 0644)
	if err != nil {
		t.Fatalf("failed to write custom global USER.md: %v", err)
	}

	err = agent.SeedGlobalUser(configDir)
	if err != nil {
		t.Fatalf("SeedGlobalUser on populated config dir failed: %v", err)
	}

	userContent, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read global USER.md after re-seed: %v", err)
	}
	if string(userContent) != customContent {
		t.Errorf("global USER.md was overwritten during re-seed. Got: %q, Expected: %q", string(userContent), customContent)
	}
}

func TestSeedBootstrap(t *testing.T) {
	tmpDir := t.TempDir()
	workspace := filepath.Join(tmpDir, "workspace")

	// 1. Fresh seed
	err := agent.SeedBootstrap(workspace)
	if err != nil {
		t.Fatalf("SeedBootstrap failed: %v", err)
	}

	path := filepath.Join(workspace, "BOOTSTRAP.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected BOOTSTRAP.md to be seeded, but it does not exist")
	}

	bootstrapContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read BOOTSTRAP.md: %v", err)
	}
	tpl, _ := agent.GetTemplate("BOOTSTRAP.md")
	if string(bootstrapContent) != tpl {
		t.Errorf("BOOTSTRAP.md content does not match template. Got: %q, Expected: %q", string(bootstrapContent), tpl)
	}

	// 2. Non-destructive check
	customContent := "custom bootstrap details"
	err = os.WriteFile(path, []byte(customContent), 0644)
	if err != nil {
		t.Fatalf("failed to write custom BOOTSTRAP.md: %v", err)
	}

	err = agent.SeedBootstrap(workspace)
	if err != nil {
		t.Fatalf("SeedBootstrap on populated workspace failed: %v", err)
	}

	bootstrapContent, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read BOOTSTRAP.md after re-seed: %v", err)
	}
	if string(bootstrapContent) != customContent {
		t.Errorf("BOOTSTRAP.md was overwritten during re-seed. Got: %q, Expected: %q", string(bootstrapContent), customContent)
	}
}
