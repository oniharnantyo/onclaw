package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/skill"
)

func TestDiscover(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "onclaw-skills-discover")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested skills
	skill1Dir := filepath.Join(tmpDir, "group1", "skillA")
	if err := os.MkdirAll(skill1Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	skill1Content := `---
name: skillA
description: desc A
---
Body A
`
	if err := os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte(skill1Content), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// Nested skill 2
	skill2Dir := filepath.Join(tmpDir, "group2", "nested", "skillB")
	if err := os.MkdirAll(skill2Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}
	skill2Content := `---
name: skillB
description: desc B
---
Body B
`
	if err := os.WriteFile(filepath.Join(skill2Dir, "skill.md"), []byte(skill2Content), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	// Discover all
	candidates, err := skill.Discover(tmpDir, "")
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Verify names
	names := make(map[string]bool)
	for _, c := range candidates {
		names[c.Name] = true
	}
	if !names["skillA"] || !names["skillB"] {
		t.Errorf("expected skillA and skillB, got %+v", names)
	}

	// Discover with restrict
	candidatesRestricted, err := skill.Discover(tmpDir, "group1")
	if err != nil {
		t.Fatalf("discover restricted failed: %v", err)
	}
	if len(candidatesRestricted) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidatesRestricted))
	}
	if candidatesRestricted[0].Name != "skillA" {
		t.Errorf("expected skillA, got %s", candidatesRestricted[0].Name)
	}
}
